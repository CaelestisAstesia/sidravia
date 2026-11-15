package app

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
)

type Application struct {
	maintenanceActive      atomic.Bool
	catalog                *config.Catalog
	profiles               *config.ProfileCatalog
	authenticationResolver *AuthenticationResolver
	supervisor             *supervisor.Supervisor
	sessionIndexMu         sync.Mutex
	operationMu            sync.Mutex // serializes configuration and Session operations
	sessionsByConfig       map[config.ConfigurationID]session.AuthenticationSessionID
	// reconnectByConfig records the immutable policy captured by the tracked Session.
	reconnectByConfig map[config.ConfigurationID]bool          // owned by operationMu
	invalidSessions   map[session.AuthenticationSessionID]bool // owned by operationMu; never reuse a retired runtime
}

func NewApplication(
	catalog *config.Catalog,
	profiles *config.ProfileCatalog,
	authenticationResolver *AuthenticationResolver,
	sessionSupervisor *supervisor.Supervisor,
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
	if sessionSupervisor == nil {
		return nil, fmt.Errorf("supervisor is required")
	}
	return &Application{
		catalog:                catalog,
		profiles:               profiles,
		authenticationResolver: authenticationResolver,
		supervisor:             sessionSupervisor,
		sessionsByConfig:       make(map[config.ConfigurationID]session.AuthenticationSessionID),
		reconnectByConfig:      make(map[config.ConfigurationID]bool),
		invalidSessions:        make(map[session.AuthenticationSessionID]bool),
	}, nil
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
	return application.supervisor.ApplySystemNetworkSnapshot(ctx, snapshot)
}
