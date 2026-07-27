package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence"
)

type DeletionFailureCode string

const (
	DeletionConfigurationInUse    DeletionFailureCode = "configuration_in_use"
	DeletionConfigurationNotFound DeletionFailureCode = "configuration_not_found"
	DeletionCatalogUnavailable    DeletionFailureCode = "catalog_unavailable"
	DeletionControllerUnavailable DeletionFailureCode = "controller_unavailable"
	DeletionInvalidArgument       DeletionFailureCode = "invalid_argument"
)

type DeletionFailure struct {
	code  DeletionFailureCode
	cause error
}

func NewDeletionFailure(code DeletionFailureCode, cause error) *DeletionFailure {
	return &DeletionFailure{code: code, cause: cause}
}

func (failure *DeletionFailure) Error() string {
	if failure.cause != nil {
		return fmt.Sprintf("%s: %v", failure.code, failure.cause)
	}
	return string(failure.code)
}

func (failure *DeletionFailure) Unwrap() error             { return failure.cause }
func (failure *DeletionFailure) Code() DeletionFailureCode { return failure.code }

type Application struct {
	catalog                *config.Catalog
	profiles               *config.ProfileCatalog
	authenticationResolver *AuthenticationResolver
	sup                    *supervisor.Supervisor
	mu                     sync.Mutex
	opMu                   sync.Mutex // serializes StartAuthentication and DeleteConfiguration
	sessionsByConfig       map[config.ConfigurationID][]session.AuthenticationSessionID
}

func NewApplication(
	catalog *config.Catalog,
	profiles *config.ProfileCatalog,
	authenticationResolver *AuthenticationResolver,
	sup *supervisor.Supervisor,
) (*Application, error) {
	if catalog == nil {
		return nil, NewDeletionFailure(DeletionInvalidArgument, fmt.Errorf("configuration catalog is required"))
	}
	if profiles == nil {
		return nil, NewDeletionFailure(DeletionInvalidArgument, fmt.Errorf("profile catalog is required"))
	}
	if authenticationResolver == nil {
		return nil, NewDeletionFailure(DeletionInvalidArgument, fmt.Errorf("authenticationResolver is required"))
	}
	if sup == nil {
		return nil, NewDeletionFailure(DeletionInvalidArgument, fmt.Errorf("supervisor is required"))
	}
	return &Application{
		catalog:                catalog,
		profiles:               profiles,
		authenticationResolver: authenticationResolver,
		sup:                    sup,
		sessionsByConfig:       make(map[config.ConfigurationID][]session.AuthenticationSessionID),
	}, nil
}

func (application *Application) StartAuthentication(ctx context.Context, configurationID config.ConfigurationID) (session.AuthenticationSessionID, session.Snapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()

	definition, err := application.authenticationResolver.Resolve(ctx, configurationID, "pending")
	if err != nil {
		return "", session.Snapshot{}, err
	}

	sessionID, snapshot, err := application.sup.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		return "", session.Snapshot{}, err
	}

	application.mu.Lock()
	application.sessionsByConfig[configurationID] = append(application.sessionsByConfig[configurationID], sessionID)
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

func (application *Application) StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	return application.sup.Stop(ctx, sessionID)
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

func (application *Application) DeleteConfiguration(ctx context.Context, id config.ConfigurationID) error {
	if ctx == nil {
		return NewDeletionFailure(DeletionInvalidArgument, fmt.Errorf("context is required"))
	}
	if err := ctx.Err(); err != nil {
		return NewDeletionFailure(DeletionInvalidArgument, err)
	}
	if id == "" {
		return NewDeletionFailure(DeletionInvalidArgument, fmt.Errorf("configuration id is required"))
	}

	application.opMu.Lock()
	defer application.opMu.Unlock()

	application.mu.Lock()
	sessionIDs := append([]session.AuthenticationSessionID(nil), application.sessionsByConfig[id]...)
	application.mu.Unlock()

	// 1. Verify all sessions are stopped via Supervisor (single authority).
	for _, sessionID := range sessionIDs {
		snapshot, err := application.sup.Get(ctx, sessionID)
		if err != nil {
			return NewDeletionFailure(DeletionControllerUnavailable, err)
		}
		if snapshot.State != session.Suspended {
			return NewDeletionFailure(DeletionConfigurationInUse, nil)
		}
	}

	// 2. Forget each stopped session; on success, immediately remove from tracking.
	for _, sessionID := range sessionIDs {
		if err := application.sup.ForgetStopped(sessionID); err != nil {
			return NewDeletionFailure(DeletionControllerUnavailable, err)
		}
		application.mu.Lock()
		application.removeSessionFromConfigLocked(id, sessionID)
		application.mu.Unlock()
	}

	// 3. Delete from catalog. If this fails, the forgotten sessions stay
	//    forgotten. A retry on the same Application will see an empty
	//    sessionsByConfig slice and proceed to catalog.Delete.
	if err := application.catalog.Delete(ctx, id); err != nil {
		var failure *persistence.Failure
		if errors.As(err, &failure) {
			switch failure.Code() {
			case persistence.FailureNotFound:
				return NewDeletionFailure(DeletionConfigurationNotFound, err)
			default:
				return NewDeletionFailure(DeletionCatalogUnavailable, err)
			}
		}
		return NewDeletionFailure(DeletionCatalogUnavailable, err)
	}

	return nil
}

func (application *Application) removeSessionFromConfigLocked(
	configID config.ConfigurationID,
	sessionID session.AuthenticationSessionID,
) {
	ids := application.sessionsByConfig[configID]
	for i, id := range ids {
		if id == sessionID {
			application.sessionsByConfig[configID] = append(ids[:i], ids[i+1:]...)
			if len(application.sessionsByConfig[configID]) == 0 {
				delete(application.sessionsByConfig, configID)
			}
			return
		}
	}
}
