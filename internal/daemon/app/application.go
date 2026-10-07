package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type ConfigurationResult struct {
	RuntimeAvailability      ConfigurationRuntimeAvailability
	Configuration            config.Configuration
	InstitutionDisplayName   string
	AuthenticationProtocolID string
	CredentialStored         bool
	StorageProtection        jsonfile.ProtectionStatus
}

type SessionStartResult struct {
	SessionID session.AuthenticationSessionID
	Snapshot  session.Snapshot
	Outcome   string
}

const (
	SessionStartCreated        = "created"
	SessionStartAlreadyRunning = "already_running"
	SessionStartResumed        = "resumed"
)

type Application struct {
	catalog                *config.Catalog
	profiles               *config.ProfileCatalog
	authenticationResolver *AuthenticationResolver
	sup                    *supervisor.Supervisor
	mu                     sync.Mutex
	opMu                   sync.Mutex // serializes configuration and Session operations
	sessionsByConfig       map[config.ConfigurationID]session.AuthenticationSessionID
	invalidSessions        map[session.AuthenticationSessionID]bool // owned by opMu; never reuse a retired runtime
}

func NewApplication(
	catalog *config.Catalog,
	profiles *config.ProfileCatalog,
	authenticationResolver *AuthenticationResolver,
	sup *supervisor.Supervisor,
) (*Application, error) {
	if catalog == nil {
		return nil, fmt.Errorf("configuration catalog is required")
	}
	if profiles == nil {
		return nil, fmt.Errorf("profile catalog is required")
	}
	if authenticationResolver == nil {
		return nil, fmt.Errorf("authenticationResolver is required")
	}
	if sup == nil {
		return nil, fmt.Errorf("supervisor is required")
	}
	return &Application{
		catalog:                catalog,
		profiles:               profiles,
		authenticationResolver: authenticationResolver,
		sup:                    sup,
		sessionsByConfig:       make(map[config.ConfigurationID]session.AuthenticationSessionID),
		invalidSessions:        make(map[session.AuthenticationSessionID]bool),
	}, nil
}

func (application *Application) StartConfigurationAuthentication(ctx context.Context, configurationID config.ConfigurationID) (SessionStartResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()

	application.mu.Lock()
	existing := application.sessionsByConfig[configurationID]
	application.mu.Unlock()
	if existing != "" && application.invalidSessions[existing] {
		if err := application.retireConfigurationSession(ctx, configurationID); err != nil {
			return SessionStartResult{}, err
		}
		existing = ""
	}
	if existing != "" {
		before, err := application.sup.Get(ctx, existing)
		if err != nil {
			return SessionStartResult{}, err
		}
		snapshot, err := application.sup.EnsureRunning(ctx, existing)
		if err != nil {
			return SessionStartResult{}, err
		}
		outcome := SessionStartAlreadyRunning
		if before.State == session.Suspended || before.State == session.Stopping {
			outcome = SessionStartResumed
		}
		return SessionStartResult{SessionID: existing, Snapshot: snapshot, Outcome: outcome}, nil
	}
	definition, err := application.authenticationResolver.Resolve(ctx, configurationID, "pending")
	if err != nil {
		return SessionStartResult{}, err
	}

	sessionID, snapshot, err := application.sup.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return SessionStartResult{}, err
	}

	application.mu.Lock()
	application.sessionsByConfig[configurationID] = sessionID
	application.mu.Unlock()

	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: SessionStartCreated}, nil
}

// StartOneShotAuthentication starts a one-shot authentication session from a
// typed request that carries its own credential. It serializes with the
// existing start/delete operation boundary, resolves the typed input, and
// starts the session through the Supervisor with MaintainAuthentication.
//
// The started session is not owned by any Configuration, so it is not tracked
// in sessionsByConfig. StopSession and GetSession work unchanged with the
// returned ID, and the existing single-active admission rule rejects a second
// active one-shot or persisted start. If resolution or Supervisor start fails,
// no session or configuration tracking entry remains.
func (application *Application) StartOneShotAuthentication(ctx context.Context, input OneShotAuthenticationInput) (SessionStartResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()

	definition, err := application.authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if err != nil {
		return SessionStartResult{}, err
	}

	sessionID, snapshot, err := application.sup.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return SessionStartResult{}, err
	}

	// One-shot sessions have no Configuration owner and are intentionally not
	// added to sessionsByConfig.
	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: SessionStartCreated}, nil
}

// PerformAutomaticLogin finds the sole AutoLogin=true Configuration, if any,
// and starts authentication through the existing StartConfigurationAuthentication
// path. It is evaluated once per daemon generation after the first accepted
// Environment Snapshot. A failure returns a fixed safe error that carries no
// configuration ID, username, error text or diagnostic cause; the caller is
// responsible for emitting the fixed Warn event. The daemon, IPC and snapshot
// delivery remain running regardless.
func (application *Application) PerformAutomaticLogin(ctx context.Context) error {
	configurations, err := application.catalog.List(ctx)
	if err != nil {
		return fmt.Errorf("automatic_login_failed")
	}
	var autoLoginID config.ConfigurationID
	for _, configuration := range configurations {
		if configuration.AutoLogin {
			autoLoginID = configuration.ConfigurationID
			break
		}
	}
	if autoLoginID == "" {
		return nil
	}
	if _, err := application.StartConfigurationAuthentication(ctx, autoLoginID); err != nil {
		return fmt.Errorf("automatic_login_failed")
	}
	return nil
}

func (application *Application) StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	return application.sup.Stop(ctx, sessionID)
}

func (application *Application) EnsureSessionRunning(ctx context.Context, sessionID session.AuthenticationSessionID) (SessionStartResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()

	if application.invalidSessions[sessionID] {
		return SessionStartResult{}, supervisor.ErrSessionStateConflict
	}
	before, err := application.sup.Get(ctx, sessionID)
	if err != nil {
		return SessionStartResult{}, err
	}
	snapshot, err := application.sup.EnsureRunning(ctx, sessionID)
	if err != nil {
		return SessionStartResult{}, err
	}
	outcome := SessionStartAlreadyRunning
	if before.State == session.Suspended || before.State == session.Stopping {
		outcome = SessionStartResumed
	}
	return SessionStartResult{SessionID: sessionID, Snapshot: snapshot, Outcome: outcome}, nil
}

func (application *Application) RestartSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if application.invalidSessions[sessionID] {
		return session.Snapshot{}, supervisor.ErrSessionStateConflict
	}
	return application.sup.Restart(ctx, sessionID)
}

func (application *Application) RemoveSession(ctx context.Context, sessionID session.AuthenticationSessionID) error {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if err := application.sup.Remove(ctx, sessionID); err != nil {
		return err
	}
	delete(application.invalidSessions, sessionID)
	application.mu.Lock()
	for configurationID, associated := range application.sessionsByConfig {
		if associated == sessionID {
			delete(application.sessionsByConfig, configurationID)
		}
	}
	application.mu.Unlock()
	return nil
}

func (application *Application) GetSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	return application.sup.Get(ctx, sessionID)
}

func (application *Application) ListSessions(ctx context.Context) ([]session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	return application.sup.List(ctx)
}

func (application *Application) ListInstitutionProfiles(ctx context.Context) ([]config.InstitutionProfileSummary, error) {
	return application.profiles.ListSummaries(ctx)
}

// ApplySystemNetworkSnapshot delegates a typed system network snapshot to the
// Supervisor. It adds no second cache, performs no binding selection and starts
// no goroutine. It deliberately does not take the start/delete operation
// mutex; the Supervisor owns the Start/Apply concurrency rule. Existing start,
// stop, get and delete behavior remains unchanged.
func (application *Application) ApplySystemNetworkSnapshot(
	ctx context.Context,
	snapshot environment.Snapshot,
) error {
	return application.sup.ApplySystemNetworkSnapshot(ctx, snapshot)
}

func (application *Application) ListConfigurations(ctx context.Context) ([]ConfigurationResult, jsonfile.ProtectionStatus, error) {
	if err := readModelContextError(ctx); err != nil {
		return nil, "", err
	}
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if err := readModelContextError(ctx); err != nil {
		return nil, "", err
	}
	values, err := application.catalog.List(ctx)
	if err != nil {
		if contextErr := readModelContextError(ctx); contextErr != nil {
			err = errors.Join(err, contextErr)
		}
		return nil, "", err
	}
	results := make([]ConfigurationResult, 0, len(values))
	for _, value := range values {
		result, err := application.describeConfiguration(ctx, value)
		if err != nil {
			return nil, "", err
		}
		results = append(results, result)
	}
	if err := readModelContextError(ctx); err != nil {
		return nil, "", err
	}
	return results, application.catalog.StorageProtection(), nil
}

func (application *Application) GetConfiguration(ctx context.Context, id config.ConfigurationID) (ConfigurationResult, error) {
	if err := readModelContextError(ctx); err != nil {
		return ConfigurationResult{}, err
	}
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if err := readModelContextError(ctx); err != nil {
		return ConfigurationResult{}, err
	}
	value, err := application.catalog.Get(ctx, id)
	if err != nil {
		if contextErr := readModelContextError(ctx); contextErr != nil {
			err = errors.Join(err, contextErr)
		}
		return ConfigurationResult{}, err
	}
	return application.describeConfiguration(ctx, value)
}

func (application *Application) CreateConfiguration(ctx context.Context, value config.Configuration, password string, allow bool) (ConfigurationResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	value = value.Clone()
	if err := application.validateConfigurationOverride(ctx, value); err != nil {
		return ConfigurationResult{}, err
	}
	if _, err := application.enrich(ctx, value); err != nil {
		return ConfigurationResult{}, err
	}
	persisted, err := application.catalog.Create(ctx, value, password, allow)
	if err != nil {
		return ConfigurationResult{}, err
	}
	return application.enrich(ctx, persisted)
}

func (application *Application) UpdateConfiguration(ctx context.Context, id config.ConfigurationID, update config.Update) (ConfigurationResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	current, err := application.catalog.Get(ctx, id)
	if err != nil {
		return ConfigurationResult{}, err
	}
	candidate := current.Clone()
	if update.ProtocolContextOverride != nil {
		candidate.ProtocolContextOverride = append(protocol.AuthenticationProtocolContextOverride(nil), (*update.ProtocolContextOverride)...)
	}
	if update.NetworkBindingPolicy != nil {
		candidate.NetworkBindingPolicy = *update.NetworkBindingPolicy
	}
	if update.DisplayName != nil {
		candidate.DisplayName = *update.DisplayName
	}
	if update.InstitutionProfileID != nil {
		candidate.InstitutionProfileID = *update.InstitutionProfileID
	}
	if update.Username != nil {
		candidate.Username = *update.Username
	}
	if update.AutoLogin != nil {
		candidate.AutoLogin = *update.AutoLogin
	}
	if update.AutoReconnect != nil {
		candidate.AutoReconnect = *update.AutoReconnect
	}
	if err := application.validateConfigurationOverride(ctx, candidate); err != nil {
		return ConfigurationResult{}, err
	}
	if _, err := application.enrich(ctx, candidate); err != nil {
		return ConfigurationResult{}, err
	}
	value, err := application.catalog.Update(ctx, id, update)
	if err != nil {
		return ConfigurationResult{}, err
	}
	// Display labels and logon policy do not affect the immutable authentication runtime.
	current.DisplayName, candidate.DisplayName = "", ""
	current.AutoLogin, candidate.AutoLogin = false, false
	if update.Password != nil || !reflect.DeepEqual(current, candidate) {
		if err := application.retireConfigurationSession(ctx, id); err != nil {
			return ConfigurationResult{}, err
		}
	}
	return application.enrich(ctx, value)
}

// ErrConfigurationSessionInvalidation distinguishes a durable edit from failed cleanup.
var ErrConfigurationSessionInvalidation = errors.New("configuration committed; session cleanup required")

func (application *Application) SetConfigurationPassword(ctx context.Context, id config.ConfigurationID, password string, allow bool) (ConfigurationResult, error) {
	return application.UpdateConfiguration(ctx, id, config.Update{Password: &password, AllowInsecureStorage: allow})
}

func (application *Application) retireConfigurationSession(ctx context.Context, id config.ConfigurationID) error {
	application.mu.Lock()
	sessionID := application.sessionsByConfig[id]
	application.mu.Unlock()
	if sessionID == "" {
		return nil
	}
	application.invalidSessions[sessionID] = true
	// Once persistence commits, caller cancellation must not skip cleanup.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	if err := application.sup.Remove(cleanup, sessionID); err != nil && !errors.Is(err, supervisor.ErrSessionNotFound) {
		return errors.Join(ErrConfigurationSessionInvalidation, err)
	}
	delete(application.invalidSessions, sessionID)
	application.mu.Lock()
	delete(application.sessionsByConfig, id)
	application.mu.Unlock()
	return nil
}

func (application *Application) RemoveConfiguration(ctx context.Context, id config.ConfigurationID, allow ...bool) error {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if err := application.catalog.Delete(ctx, id, allow...); err != nil {
		return err
	}
	return application.retireConfigurationSession(ctx, id)
}

func (application *Application) enrich(ctx context.Context, value config.Configuration) (ConfigurationResult, error) {
	profile, err := application.profiles.Get(ctx, value.InstitutionProfileID)
	if err != nil {
		var failure *persistence.Failure
		if errors.As(err, &failure) && failure.Code() == persistence.FailureNotFound {
			return ConfigurationResult{}, NewResolutionFailure(ProfileNotFound, err)
		}
		return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	if _, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID); err != nil {
		return ConfigurationResult{}, NewResolutionFailure(ProtocolNotFound, err)
	}
	stored := false
	if value.ConfigurationID != "" {
		var err error
		stored, err = application.catalog.HasRecord(ctx, value.ConfigurationID)
		if err != nil {
			return ConfigurationResult{}, err
		}
	}
	return ConfigurationResult{
		RuntimeAvailability: ConfigurationRuntimeAvailable, Configuration: value.Clone(), InstitutionDisplayName: profile.DisplayName,
		AuthenticationProtocolID: string(profile.AuthenticationProtocolID),
		CredentialStored:         stored, StorageProtection: application.catalog.StorageProtection(),
	}, nil
}

// validateConfigurationOverride is write-only validation of public candidate
// data. It never resolves stored credentials or creates a protocol Run, and
// leaves legacy read-only enrichment unchanged.
func (application *Application) validateConfigurationOverride(ctx context.Context, value config.Configuration) error {
	profile, err := application.profiles.Get(ctx, value.InstitutionProfileID)
	if err != nil {
		var failure *persistence.Failure
		if errors.As(err, &failure) && failure.Code() == persistence.FailureNotFound {
			return NewResolutionFailure(ProfileNotFound, err)
		}
		return NewResolutionFailure(InvalidConfiguration, err)
	}
	factory, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID)
	if err != nil {
		return NewResolutionFailure(ProtocolNotFound, err)
	}
	if err := factory.ValidateProtocolContextOverride(append(protocol.AuthenticationProtocolContextOverride(nil), value.ProtocolContextOverride...)); err != nil {
		return NewResolutionFailure(InvalidConfiguration, err)
	}
	return nil
}
