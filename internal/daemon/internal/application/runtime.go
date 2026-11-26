package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"sidravia/internal/connection/auth/protocol"
	"sidravia/internal/connection/auth/supported"
	"sidravia/internal/connection/environment"
	profilecatalog "sidravia/internal/connection/profile/catalog"
	"sidravia/internal/daemon/internal/application/internal/configstore"
	"sidravia/internal/daemon/internal/supervisor"
	"sidravia/internal/document/securefile"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

type Paths struct {
	Configurations string
	Profiles       string
}

// Runtime is the application surface for the daemon composition. Configuration
// storage, credential resolution and typed mutations stay inside application.
type Runtime struct {
	application *Application
	ipc         *ipcDispatcher
}

func Open(
	ctx context.Context,
	store securefile.SensitiveStore,
	paths Paths,
	protocols *protocol.AuthenticationProtocolRegistry,
	host environment.SystemHostInformation,
	dependencies supervisor.Dependencies,
	version, buildID string,
	options launchcontract.Options,
) (*Runtime, error) {
	// Secure configuration-root preparation must precede profile traversal.
	configurations, err := configstore.OpenCatalog(ctx, store, paths.Configurations)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: open catalog: %w", err)
	}
	profiles, err := profilecatalog.LoadProfileCatalogFromDirectory(ctx, paths.Profiles, supported.ValidateInstitution)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: load profiles: %w", err)
	}

	sessions := supervisor.New(dependencies)
	resolver, err := newAuthenticationResolver(configurations, profiles, protocols, host)
	if err != nil {
		return nil, closeOnOpenFailure(sessions, fmt.Errorf("sidraviad: create resolver: %w", err))
	}
	application, err := newApplication(configurations, profiles, resolver, sessions)
	if err != nil {
		return nil, closeOnOpenFailure(sessions, fmt.Errorf("sidraviad: create application: %w", err))
	}
	return &Runtime{application: application, ipc: newIPCDispatcher(application, version, buildID, options, openMaintenanceCoordinator)}, nil
}

func (runtime *Runtime) HandleIPC(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	return runtime.ipc.handle(ctx, method, payload)
}

func (runtime *Runtime) SubscribeStateEvents(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
	return runtime.application.SubscribeStateEvents(ctx)
}

func (runtime *Runtime) ApplySystemNetworkSnapshot(ctx context.Context, snapshot environment.Snapshot) error {
	return runtime.application.ApplySystemNetworkSnapshot(ctx, snapshot)
}

func (runtime *Runtime) PerformAutomaticLogin(ctx context.Context) error {
	return runtime.application.PerformAutomaticLogin(ctx)
}

func (runtime *Runtime) Close() error { return runtime.application.supervisor.Close() }
func (runtime *Runtime) Wait() {
	runtime.application.supervisor.Wait()
	runtime.ipc.close()
}

func closeOnOpenFailure(sessions *supervisor.Supervisor, cause error) error {
	closeErr := sessions.Close()
	sessions.Wait()
	if closeErr != nil {
		return errors.Join(cause, fmt.Errorf("sidraviad: supervisor close after composition failure: %w", closeErr))
	}
	return cause
}
