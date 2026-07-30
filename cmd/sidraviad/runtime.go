package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"sidravia/internal/daemon/app"
	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	"sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/host"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/ipc/server"
	"sidravia/internal/productlayout"
)

// Operational event codes and fixed Simplified Chinese messages for the daemon
// process boundary. Messages are constant summaries; they are never constructed
// from an error, snapshot, request or response. Only the attributes listed in
// the stable schema ever appear on a record.
const (
	eventDaemonStartFailed      = "daemon_start_failed"
	eventDaemonRuntimeStarted   = "daemon_runtime_started"
	eventDaemonRuntimeFailed    = "daemon_runtime_failed"
	eventDaemonRuntimeStopped   = "daemon_runtime_stopped"
	eventNetworkSnapshotApplied = "network_snapshot_applied"
	eventDaemonStopRequested    = "daemon_stop_requested"

	msgDaemonStartFailed      = "守护进程启动失败"
	msgDaemonRuntimeStarted   = "守护进程运行已启动"
	msgDaemonRuntimeFailed    = "守护进程运行失败"
	msgDaemonRuntimeStopped   = "守护进程运行已停止"
	msgNetworkSnapshotApplied = "网络快照已应用"
	msgDaemonStopRequested    = "守护进程停止请求已提交"
)

type defaultPaths struct {
	profiles       string
	configurations string
	runtimeInfo    string
	portable       bool
}

type networkSnapshotSink interface {
	ApplySystemNetworkSnapshot(context.Context, environment.Snapshot) error
}

type shutdownBoundary interface {
	Close() error
	Wait()
}

type composedRuntime struct {
	observer       environment.Observer
	snapshotSink   networkSnapshotSink
	shutdown       shutdownBoundary
	hostCfg        host.Config
	hostRunner     func(context.Context, host.Config) error
	handler        server.Handler
	logger         *slog.Logger
	productVersion string
	buildID        string
	stopCh         chan struct{}
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
	layout, err := productlayout.Resolve()
	if err != nil {
		return defaultPaths{}, fmt.Errorf("sidraviad: derive default paths: %w", err)
	}
	return defaultPaths{
		profiles:       layout.InstitutionProfilesDirectory,
		configurations: layout.ConfigurationsPath,
		runtimeInfo:    layout.RuntimeInfoPath,
		portable:       layout.Mode == productlayout.ModePortable,
	}, nil
}

func constructProductionSystem(ctx context.Context, logger *slog.Logger) (*composedRuntime, error) {
	if logger == nil {
		return nil, errors.New("sidraviad: logger is required")
	}
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

	onUnprotected := newStorageProtectionWarning(logger)
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{
		IntendedOwnerSID:                   "",
		AllowUnsupportedProtectionFallback: paths.portable,
		OnUnprotected:                      onUnprotected,
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

	return composeObjectGraph(ctx, store, paths, hostInfo, observer, host.Run, token, ProductVersion, BuildID, logger, onUnprotected)
}

func composeObjectGraph(
	ctx context.Context,
	store configuration.SensitiveStore,
	paths defaultPaths,
	hostInfo environment.SystemHostInformation,
	observer environment.Observer,
	hostRunner func(context.Context, host.Config) error,
	token string,
	productVersion string,
	buildID string,
	logger *slog.Logger,
	unprotectedCallbacks ...func(),
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
	if logger == nil {
		return nil, errors.New("sidraviad: logger is required")
	}
	if hostInfo.HostName == "" {
		return nil, errors.New("sidraviad: host name is required")
	}
	var onUnprotected func()
	if len(unprotectedCallbacks) > 0 {
		onUnprotected = unprotectedCallbacks[0]
	}
	for name, path := range map[string]string{
		"profiles":       paths.profiles,
		"configurations": paths.configurations,
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

	// Open the Authentication Configuration catalog before loading institution
	// Profiles. OpenCatalog performs the secure store Read, which prepares and
	// repairs the shared configuration root (including inheritance repair of any
	// pre-existing institution-profiles subtree). Only after that succeeds may
	// the Profile loader traverse <config-root>/institution-profiles.
	catalog, err := configuration.OpenCatalog(ctx, store, paths.configurations)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: open catalog: %w", err)
	}

	profiles, err := configuration.LoadProfileCatalogFromDirectory(ctx, paths.profiles, registry)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: load profiles: %w", err)
	}

	sup := supervisor.New(supervisor.Dependencies{
		RetryPolicy:                session.NewDefaultRetryPolicy(),
		RetryScheduler:             session.NewTimerRetryScheduler(),
		Diagnostics:                newSessionDiagnostics(logger),
		ProtocolDiagnosticsFactory: newProtocolDiagnosticsFactory(logger),
	})

	resolver, err := app.NewAuthenticationResolver(catalog, profiles, registry, hostInfo)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create resolver: %w", err))
	}

	application, err := app.NewApplication(catalog, profiles, resolver, sup)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create application: %w", err))
	}

	handler := app.IPCHandler(application, productVersion, buildID)
	stopCh := make(chan struct{}, 1)
	var stopOnce sync.Once
	srv, err := server.NewServer(token, buildID, handler, logger, func(method string) {
		// A committed daemon.stop success response has been written to the
		// client. Signal the runtime-owned stop channel once and
		// non-blockingly; the runtime then cancels its child context and runs
		// the existing graceful shutdown path. The server knows nothing about
		// daemon lifecycle semantics.
		if method == contract.MethodDaemonStop {
			stopOnce.Do(func() { stopCh <- struct{}{} })
		}
	})
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create ipc server: %w", err))
	}

	hostCfg := host.Config{
		ProductVersion:                     productVersion,
		BuildID:                            buildID,
		Token:                              token,
		RuntimeInfoPath:                    paths.runtimeInfo,
		Handler:                            http.HandlerFunc(srv.ServeHTTP),
		AllowUnsupportedProtectionFallback: paths.portable,
		OnUnprotected:                      onUnprotected,
	}

	return &composedRuntime{
		observer:       observer,
		snapshotSink:   application,
		shutdown:       sup,
		hostCfg:        hostCfg,
		hostRunner:     hostRunner,
		handler:        handler,
		logger:         logger,
		productVersion: productVersion,
		buildID:        buildID,
		stopCh:         stopCh,
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
		// A nil context is a programming error before runtime start. The runtime
		// never started, so it emits neither a started nor a stopped event; the
		// caller owns the single daemon_runtime_failed record.
		return rt.closeAndWait(errors.New("sidraviad: nil context"))
	}
	if err := ctx.Err(); err != nil {
		return rt.closeAndWait(nil)
	}

	rt.logger.Info(msgDaemonRuntimeStarted,
		slog.String("event", eventDaemonRuntimeStarted),
		slog.String("product_version", rt.productVersion),
		slog.String("build_id", rt.buildID),
		slog.Int("pid", os.Getpid()),
	)

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
	inspectRemaining := false
	select {
	case <-ctx.Done():
		inspectRemaining = true
	case <-rt.stopCh:
		// A committed daemon.stop response has been written to the client. Begin
		// graceful shutdown: cancel the child context so host/observer/delivery
		// exit, then close and wait for Supervisor through the existing path.
		rt.logger.Info(msgDaemonStopRequested, slog.String("event", eventDaemonStopRequested))
		inspectRemaining = false
	case result := <-results:
		received = 1
		initiator = classifyRuntimeResult(ctx, result)
		inspectRemaining = ctx.Err() != nil
	}

	cancel()

	for received < 3 {
		result := <-results
		received++
		if inspectRemaining && initiator == nil {
			initiator = classifyRuntimeResult(ctx, result)
		}
	}

	finalErr := rt.closeAndWait(initiator)
	if finalErr == nil {
		rt.logger.Info(msgDaemonRuntimeStopped, slog.String("event", eventDaemonRuntimeStopped))
	}
	// A failed runtime does not also log the normal stopped event. The single
	// daemon_runtime_failed record is owned by the process boundary (runMain) so
	// the original cause stays out of the log while error propagation is kept.
	return finalErr
}

func classifyRuntimeResult(ctx context.Context, result runtimeActivityResult) error {
	if callerErr := ctx.Err(); callerErr != nil &&
		(result.err == nil || errors.Is(result.err, callerErr)) {
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
			rt.logger.Info(msgNetworkSnapshotApplied,
				slog.String("event", eventNetworkSnapshotApplied),
				slog.Uint64("revision", snapshot.Revision),
				slog.Int("interface_count", len(snapshot.Interfaces())),
			)
		}
	}
}
