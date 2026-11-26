package application

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	connectionconfig "sidravia/internal/connection/config"
	"sidravia/internal/connection/environment"
	profilevalue "sidravia/internal/connection/profile"
	profilecatalog "sidravia/internal/connection/profile/catalog"
	config "sidravia/internal/daemon/internal/application/internal/configstore"
	"sidravia/internal/daemon/internal/session"
	"sidravia/internal/daemon/internal/supervisor"
)

type Application struct {
	maintenanceActive      atomic.Bool
	catalog                *config.Catalog
	profiles               *profilecatalog.ProfileCatalog
	authenticationResolver *AuthenticationResolver
	supervisor             *supervisor.Supervisor
	// operationMu serializes configuration/session operations and coherent reads.
	// Acquire it before sessionIndexMu when both are needed.
	sessionIndexMu   sync.Mutex // protects sessionsByConfig; released before Supervisor calls
	operationMu      sync.Mutex
	sessionsByConfig map[connectionconfig.ID]session.AuthenticationSessionID
	// reconnectByConfig records the immutable policy captured by the tracked Session.
	reconnectByConfig map[connectionconfig.ID]bool             // owned by operationMu
	invalidSessions   map[session.AuthenticationSessionID]bool // owned by operationMu; never reuse a retired runtime
}

func newApplication(
	catalog *config.Catalog,
	profiles *profilecatalog.ProfileCatalog,
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
		sessionsByConfig:       make(map[connectionconfig.ID]session.AuthenticationSessionID),
		reconnectByConfig:      make(map[connectionconfig.ID]bool),
		invalidSessions:        make(map[session.AuthenticationSessionID]bool),
	}, nil
}

func (application *Application) ListInstitutionProfiles(ctx context.Context) ([]profilevalue.Summary, error) {
	summaries, err := application.profiles.ListSummaries(ctx)
	if err != nil {
		return nil, err
	}
	for i := range summaries {
		factory, err := application.authenticationResolver.protocols.GetFactory(summaries[i].AuthenticationProtocolID)
		if err == nil {
			summaries[i].OverrideSchemaVersion = overrideSchemaVersion(factory)
		}
	}
	return summaries, nil
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
