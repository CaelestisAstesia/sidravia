package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"sidravia/internal/daemon/app"
	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	"sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/host"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/server"
)

type defaultPaths struct {
	profiles       string
	configurations string
	credentials    string
	runtimeInfo    string
}

type networkSnapshotSink interface {
	ApplySystemNetworkSnapshot(context.Context, environment.Snapshot) error
}

type shutdownBoundary interface {
	Close() error
	Wait()
}

type composedRuntime struct {
	observer     environment.Observer
	snapshotSink networkSnapshotSink
	shutdown     shutdownBoundary
	hostCfg      host.Config
	hostRunner   func(context.Context, host.Config) error
	handler      server.Handler
}

type runtimeActivity uint8

const (
	runtimeActivityHost runtimeActivity = iota
	runtimeActivityObserver
	runtimeActivityDelivery
)

type runtimeActivityResult struct {
	activity runtimeActivity
	err      error
}

func deriveDefaultPaths() (defaultPaths, error) {
	profilesDir, err := configuration.DefaultInstitutionProfilesDirectory()
	if err != nil {
		return defaultPaths{}, fmt.Errorf("derive profiles directory: %w", err)
	}
	parent := filepath.Dir(profilesDir)
	configurationsPath := filepath.Join(parent, "configurations.json")
	credentialsPath := filepath.Join(parent, "credentials.json")
	runtimeInfoPath, err := host.DefaultRuntimeInfoPath()
	if err != nil {
		return defaultPaths{}, fmt.Errorf("derive runtime info path: %w", err)
	}
	return defaultPaths{
		profiles:       profilesDir,
		configurations: configurationsPath,
		credentials:    credentialsPath,
		runtimeInfo:    runtimeInfoPath,
	}, nil
}

func constructProductionSystem(ctx context.Context) (*composedRuntime, error) {
	if ctx == nil {
		return nil, errors.New("sidraviad: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sidraviad: context cancelled: %w", err)
	}
	if ProductVersion == "" {
		return nil, errors.New("sidraviad: ProductVersion is required")
	}
	if BuildID == "" {
		return nil, errors.New("sidraviad: BuildID is required")
	}

	paths, err := deriveDefaultPaths()
	if err != nil {
		return nil, err
	}

	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{
		IntendedOwnerSID: "",
	})
	if err != nil {
		return nil, fmt.Errorf("sidraviad: create secure store: %w", err)
	}

	hostInfo, err := environment.ReadSystemHostInformation()
	if err != nil {
		return nil, fmt.Errorf("sidraviad: read host information: %w", err)
	}

	observer := environment.NewSystemObserver()

	token, err := host.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("sidraviad: generate token: %w", err)
	}

	return composeObjectGraph(ctx, store, paths, hostInfo, observer, host.Run, token, ProductVersion, BuildID)
}

func composeObjectGraph(
	ctx context.Context,
	store jsonfile.Store,
	paths defaultPaths,
	hostInfo environment.SystemHostInformation,
	observer environment.Observer,
	hostRunner func(context.Context, host.Config) error,
	token string,
	productVersion string,
	buildID string,
) (*composedRuntime, error) {
	if ctx == nil {
		return nil, errors.New("sidraviad: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sidraviad: context cancelled: %w", err)
	}
	if store == nil {
		return nil, errors.New("sidraviad: store is required")
	}
	if observer == nil {
		return nil, errors.New("sidraviad: observer is required")
	}
	if hostRunner == nil {
		return nil, errors.New("sidraviad: host runner is required")
	}
	if token == "" {
		return nil, errors.New("sidraviad: token is required")
	}
	if productVersion == "" || buildID == "" {
		return nil, errors.New("sidraviad: product version and build ID are required")
	}
	if hostInfo.HostName == "" {
		return nil, errors.New("sidraviad: host name is required")
	}
	for name, path := range map[string]string{
		"profiles":       paths.profiles,
		"configurations": paths.configurations,
		"credentials":    paths.credentials,
		"runtime info":   paths.runtimeInfo,
	} {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, fmt.Errorf("sidraviad: %s path must be absolute and clean", name)
		}
	}

	registry, err := protocol.NewAuthenticationProtocolRegistry(d520.NewFactory())
	if err != nil {
		return nil, fmt.Errorf("sidraviad: create protocol registry: %w", err)
	}

	profiles, err := configuration.LoadProfileCatalogFromDirectory(ctx, paths.profiles, registry)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: load profiles: %w", err)
	}

	catalog, err := configuration.OpenCatalog(ctx, store, paths.configurations)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: open catalog: %w", err)
	}

	credStore, err := credentials.OpenStore(ctx, store, paths.credentials)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: open credentials: %w", err)
	}

	sup := supervisor.New(supervisor.Dependencies{
		RetryPolicy:    session.NewDefaultRetryPolicy(),
		RetryScheduler: session.NewTimerRetryScheduler(),
	})

	resolver, err := app.NewAuthenticationResolver(catalog, profiles, credStore, registry, hostInfo)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create resolver: %w", err))
	}

	application, err := app.NewApplication(catalog, resolver, sup)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create application: %w", err))
	}

	handler := app.IPCHandler(application, productVersion, buildID)
	srv := server.NewServer(token, buildID, handler)

	hostCfg := host.Config{
		ProductVersion:  productVersion,
		BuildID:         buildID,
		Token:           token,
		RuntimeInfoPath: paths.runtimeInfo,
		Handler:         http.HandlerFunc(srv.ServeHTTP),
	}

	return &composedRuntime{
		observer:     observer,
		snapshotSink: application,
		shutdown:     sup,
		hostCfg:      hostCfg,
		hostRunner:   hostRunner,
		handler:      handler,
	}, nil
}

func closeAfterCompositionFailure(shutdown shutdownBoundary, cause error) error {
	closeErr := shutdown.Close()
	shutdown.Wait()
	if closeErr != nil {
		return errors.Join(cause, fmt.Errorf("sidraviad: supervisor close after composition failure: %w", closeErr))
	}
	return cause
}

func (rt *composedRuntime) run(ctx context.Context) error {
	if ctx == nil {
		return rt.closeAndWait(errors.New("sidraviad: nil context"))
	}
	if err := ctx.Err(); err != nil {
		return rt.closeAndWait(nil)
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	snapshotCh := make(chan environment.Snapshot, 1)
	results := make(chan runtimeActivityResult, 3)

	go func() {
		results <- runtimeActivityResult{
			activity: runtimeActivityHost,
			err:      rt.hostRunner(childCtx, rt.hostCfg),
		}
	}()

	go func() {
		results <- runtimeActivityResult{
			activity: runtimeActivityObserver,
			err:      rt.observer.Observe(childCtx, snapshotCh),
		}
	}()

	go func() {
		results <- runtimeActivityResult{
			activity: runtimeActivityDelivery,
			err:      rt.deliverSnapshots(childCtx, snapshotCh),
		}
	}()

	var initiator error
	received := 0
	select {
	case <-ctx.Done():
	case result := <-results:
		received = 1
		initiator = classifyRuntimeResult(ctx, result)
	}

	cancel()

	for received < 3 {
		<-results
		received++
	}

	return rt.closeAndWait(initiator)
}

func classifyRuntimeResult(ctx context.Context, result runtimeActivityResult) error {
	if ctx.Err() != nil {
		return nil
	}

	switch result.activity {
	case runtimeActivityHost:
		if result.err == nil {
			return nil
		}
		return fmt.Errorf("sidraviad: host: %w", result.err)
	case runtimeActivityObserver:
		if result.err == nil {
			return errors.New("sidraviad: observer stopped unexpectedly")
		}
		return fmt.Errorf("sidraviad: observer: %w", result.err)
	case runtimeActivityDelivery:
		if result.err == nil {
			return errors.New("sidraviad: snapshot delivery stopped unexpectedly")
		}
		return fmt.Errorf("sidraviad: snapshot delivery: %w", result.err)
	default:
		return errors.New("sidraviad: unknown runtime activity stopped")
	}
}

func (rt *composedRuntime) closeAndWait(initiator error) error {
	closeErr := rt.shutdown.Close()
	rt.shutdown.Wait()
	if closeErr == nil {
		return initiator
	}
	wrapped := fmt.Errorf("sidraviad: supervisor close: %w", closeErr)
	if initiator != nil {
		return errors.Join(initiator, wrapped)
	}
	return wrapped
}

func (rt *composedRuntime) deliverSnapshots(ctx context.Context, snapshotCh <-chan environment.Snapshot) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case snapshot, ok := <-snapshotCh:
			if !ok {
				return nil
			}
			if err := rt.snapshotSink.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("deliver snapshot: %w", err)
			}
		}
	}
}
