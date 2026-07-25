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

type composedRuntime struct {
	observer   environment.Observer
	app        *app.Application
	sup        *supervisor.Supervisor
	hostCfg    host.Config
	hostRunner func(context.Context, host.Config) error
	handler    server.Handler
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

	registry, err := protocol.NewAuthenticationProtocolRegistry(d520.NewFactory())
	if err != nil {
		return nil, fmt.Errorf("sidraviad: create protocol registry: %w", err)
	}

	var sup *supervisor.Supervisor
	sup = supervisor.New(supervisor.Dependencies{
		RetryPolicy:    session.NewDefaultRetryPolicy(),
		RetryScheduler: session.NewTimerRetryScheduler(),
	})

	profiles, err := configuration.LoadProfileCatalogFromDirectory(ctx, paths.profiles, registry)
	if err != nil {
		sup.Close()
		sup.Wait()
		return nil, fmt.Errorf("sidraviad: load profiles: %w", err)
	}

	catalog, err := configuration.OpenCatalog(ctx, store, paths.configurations)
	if err != nil {
		sup.Close()
		sup.Wait()
		return nil, fmt.Errorf("sidraviad: open catalog: %w", err)
	}

	credStore, err := credentials.OpenStore(ctx, store, paths.credentials)
	if err != nil {
		sup.Close()
		sup.Wait()
		return nil, fmt.Errorf("sidraviad: open credentials: %w", err)
	}

	resolver, err := app.NewAuthenticationResolver(catalog, profiles, credStore, registry, hostInfo)
	if err != nil {
		sup.Close()
		sup.Wait()
		return nil, fmt.Errorf("sidraviad: create resolver: %w", err)
	}

	application, err := app.NewApplication(catalog, resolver, sup)
	if err != nil {
		sup.Close()
		sup.Wait()
		return nil, fmt.Errorf("sidraviad: create application: %w", err)
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
		observer:   observer,
		app:        application,
		sup:        sup,
		hostCfg:    hostCfg,
		hostRunner: hostRunner,
		handler:    handler,
	}, nil
}

func (rt *composedRuntime) run(ctx context.Context) error {
	if ctx == nil {
		rt.sup.Close()
		rt.sup.Wait()
		return errors.New("sidraviad: nil context")
	}
	if err := ctx.Err(); err != nil {
		rt.sup.Close()
		rt.sup.Wait()
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	hostDone := make(chan error, 1)
	observerDone := make(chan error, 1)
	deliveryDone := make(chan error, 1)
	snapshotCh := make(chan environment.Snapshot, 1)

	go func() {
		hostDone <- rt.hostRunner(childCtx, rt.hostCfg)
	}()

	go func() {
		observerDone <- rt.observer.Observe(childCtx, snapshotCh)
	}()

	go func() {
		deliveryDone <- rt.deliverSnapshots(childCtx, snapshotCh)
	}()

	var initiator error
	select {
	case err := <-hostDone:
		if err != nil {
			initiator = fmt.Errorf("sidraviad: host: %w", err)
		}
	case err := <-observerDone:
		if err != nil {
			initiator = fmt.Errorf("sidraviad: observer: %w", err)
		}
	case err := <-deliveryDone:
		if err != nil {
			initiator = fmt.Errorf("sidraviad: snapshot delivery: %w", err)
		}
	}

	cancel()

	for i := 0; i < 2; i++ {
		select {
		case <-hostDone:
		case <-observerDone:
		case <-deliveryDone:
		}
	}

	if closeErr := rt.sup.Close(); closeErr != nil {
		if initiator != nil {
			initiator = errors.Join(initiator, fmt.Errorf("sidraviad: supervisor close: %w", closeErr))
		} else {
			initiator = fmt.Errorf("sidraviad: supervisor close: %w", closeErr)
		}
	}
	rt.sup.Wait()

	return initiator
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
			if err := rt.app.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
				return fmt.Errorf("deliver snapshot: %w", err)
			}
		}
	}
}
