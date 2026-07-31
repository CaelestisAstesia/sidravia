package app

import (
	"context"
	"fmt"
	"sync"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type ConfigurationResult struct {
	Configuration            config.Configuration
	InstitutionDisplayName   string
	AuthenticationProtocolID string
	CredentialStored         bool
	StorageProtection        jsonfile.ProtectionStatus
}

type Application struct {
	catalog                *config.Catalog
	profiles               *config.ProfileCatalog
	authenticationResolver *AuthenticationResolver
	sup                    *supervisor.Supervisor
	mu                     sync.Mutex
	opMu                   sync.Mutex // serializes configuration and Session operations
	sessionsByConfig       map[config.ConfigurationID]session.AuthenticationSessionID
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
	}, nil
}

func (application *Application) StartConfigurationAuthentication(ctx context.Context, configurationID config.ConfigurationID) (session.AuthenticationSessionID, session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()

	application.mu.Lock()
	existing := application.sessionsByConfig[configurationID]
	application.mu.Unlock()
	if existing != "" {
		snapshot, err := application.sup.EnsureRunning(ctx, existing)
		return existing, snapshot, err
	}
	definition, err := application.authenticationResolver.Resolve(ctx, configurationID, "pending")
	if err != nil {
		return "", session.Snapshot{}, err
	}

	sessionID, snapshot, err := application.sup.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return "", session.Snapshot{}, err
	}

	application.mu.Lock()
	application.sessionsByConfig[configurationID] = sessionID
	application.mu.Unlock()

	return sessionID, snapshot, nil
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
func (application *Application) StartOneShotAuthentication(ctx context.Context, input OneShotAuthenticationInput) (session.AuthenticationSessionID, session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()

	definition, err := application.authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if err != nil {
		return "", session.Snapshot{}, err
	}

	sessionID, snapshot, err := application.sup.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return "", session.Snapshot{}, err
	}

	// One-shot sessions have no Configuration owner and are intentionally not
	// added to sessionsByConfig.
	return sessionID, snapshot, nil
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
	if _, _, err := application.StartConfigurationAuthentication(ctx, autoLoginID); err != nil {
		return fmt.Errorf("automatic_login_failed")
	}
	return nil
}

func (application *Application) StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	return application.sup.Stop(ctx, sessionID)
}

func (application *Application) EnsureSessionRunning(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	return application.sup.EnsureRunning(ctx, sessionID)
}

func (application *Application) RestartSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	return application.sup.Restart(ctx, sessionID)
}

func (application *Application) RemoveSession(ctx context.Context, sessionID session.AuthenticationSessionID) error {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if err := application.sup.Remove(ctx, sessionID); err != nil {
		return err
	}
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
	return application.sup.Get(ctx, sessionID)
}

func (application *Application) ListSessions(ctx context.Context) ([]session.Snapshot, error) {
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
	values, err := application.catalog.List(ctx)
	if err != nil {
		return nil, "", err
	}
	results := make([]ConfigurationResult, 0, len(values))
	for _, value := range values {
		result, err := application.enrich(ctx, value)
		if err != nil {
			return nil, "", err
		}
		results = append(results, result)
	}
	return results, application.catalog.StorageProtection(), nil
}

func (application *Application) GetConfiguration(ctx context.Context, id config.ConfigurationID) (ConfigurationResult, error) {
	value, err := application.catalog.Get(ctx, id)
	if err != nil {
		return ConfigurationResult{}, err
	}
	return application.enrich(ctx, value)
}

func (application *Application) CreateConfiguration(ctx context.Context, value config.Configuration, password string, allow bool) (ConfigurationResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if _, err := application.enrich(ctx, value); err != nil {
		return ConfigurationResult{}, err
	}
	if err := application.catalog.Create(ctx, value, password, allow); err != nil {
		return ConfigurationResult{}, err
	}
	return application.enrich(ctx, value)
}

func (application *Application) UpdateConfiguration(ctx context.Context, id config.ConfigurationID, update config.Update) (ConfigurationResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	current, err := application.catalog.Get(ctx, id)
	if err != nil {
		return ConfigurationResult{}, err
	}
	candidate := current
	if update.DisplayName != nil {
		candidate.DisplayName = *update.DisplayName
	}
	if update.InstitutionProfileID != nil {
		candidate.InstitutionProfileID = *update.InstitutionProfileID
	}
	if update.Username != nil {
		candidate.Username = *update.Username
	}
	if _, err := application.enrich(ctx, candidate); err != nil {
		return ConfigurationResult{}, err
	}
	value, err := application.catalog.Update(ctx, id, update)
	if err != nil {
		return ConfigurationResult{}, err
	}
	return application.enrich(ctx, value)
}

func (application *Application) SetConfigurationPassword(ctx context.Context, id config.ConfigurationID, password string, allow bool) (ConfigurationResult, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	value, err := application.catalog.SetPassword(ctx, id, password, allow)
	if err != nil {
		return ConfigurationResult{}, err
	}
	return application.enrich(ctx, value)
}

func (application *Application) RemoveConfiguration(ctx context.Context, id config.ConfigurationID) error {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	if _, err := application.catalog.Get(ctx, id); err != nil {
		return err
	}
	application.mu.Lock()
	sessionID := application.sessionsByConfig[id]
	application.mu.Unlock()
	if sessionID != "" {
		if err := application.sup.Remove(ctx, sessionID); err != nil {
			return err
		}
		application.mu.Lock()
		delete(application.sessionsByConfig, id)
		application.mu.Unlock()
	}
	return application.catalog.Delete(ctx, id)
}

func (application *Application) enrich(ctx context.Context, value config.Configuration) (ConfigurationResult, error) {
	profile, err := application.profiles.Get(ctx, value.InstitutionProfileID)
	if err != nil {
		return ConfigurationResult{}, err
	}
	if _, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID); err != nil {
		return ConfigurationResult{}, err
	}
	return ConfigurationResult{
		Configuration: value.Clone(), InstitutionDisplayName: profile.DisplayName,
		AuthenticationProtocolID: string(profile.AuthenticationProtocolID),
		CredentialStored:         true, StorageProtection: application.catalog.StorageProtection(),
	}, nil
}
