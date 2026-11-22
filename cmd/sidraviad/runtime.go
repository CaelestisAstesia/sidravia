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
	"sidravia/internal/daemon/desktopowner"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/host"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/ipc/server"
	"sidravia/internal/launchcontract"
	"sidravia/internal/persistence/jsonfile"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/operations"
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
	eventAutomaticLoginFailed   = "automatic_login_failed"

	msgDaemonStartFailed      = "守护进程启动失败"
	msgDaemonRuntimeStarted   = "守护进程运行已启动"
	msgDaemonRuntimeFailed    = "守护进程运行失败"
	msgDaemonRuntimeStopped   = "守护进程运行已停止"
	msgNetworkSnapshotApplied = "网络快照已应用"
	msgDaemonStopRequested    = "守护进程停止请求已提交"
	msgAutomaticLoginFailed   = "自动登录失败"
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

type automaticLoginPerformer interface {
	PerformAutomaticLogin(context.Context) error
}

type shutdownBoundary interface {
	Close() error
	Wait()
}

type ipcShutdownBoundary interface {
	Shutdown(context.Context) error
}

type composedRuntime struct {
	observer       environment.Observer
	snapshotSink   networkSnapshotSink
	automaticLogin automaticLoginPerformer
	shutdown       shutdownBoundary
	ipcShutdown    ipcShutdownBoundary
	hostCfg        host.Config
	hostRunner     func(context.Context, host.Config) error
	logger         *slog.Logger
	productVersion string
	buildID        string
	launchOptions  launchcontract.Options
	desktopOwner   desktopowner.Watcher
	stopCh         chan struct{}
	networkTrace   *networkTraceRecorder
}

func deriveDefaultPaths(namespace string) (defaultPaths, error) {
	return deriveLaunchPaths(namespace, "")
}

func deriveLaunchPaths(namespace, packageMode string) (defaultPaths, error) {
	ns, err := productlayout.NewNamespace(namespace)
	if err != nil {
		return defaultPaths{}, fmt.Errorf("sidraviad: invalid runtime namespace: %w", err)
	}
	layout, err := productlayout.ResolveNamespace(ns)
	if err != nil {
		return defaultPaths{}, fmt.Errorf("sidraviad: derive default paths: %w", err)
	}
	if packageMode != "" && (string(layout.Mode) != packageMode ||
		(layout.Mode == productlayout.ModeInstalled && !productlayout.RPMManaged(layout))) {
		return defaultPaths{}, errors.New("sidraviad: registered package layout changed")
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

	launchOptions, err := launchcontract.Parse(
		os.Getenv(launchcontract.EnvMode),
		os.Getenv(launchcontract.EnvDesktopOwnerPID),
		os.Getenv(launchcontract.EnvNamespace),
	)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: parse launch options: %w", err)
	}
	launchOptions.SkipAutoLogin = os.Getenv(launchcontract.EnvSkipAutoLogin) == "1"
	launchOptions.RecoveryToken = os.Getenv(operations.EnvRecoveryToken)
	launchOptions.RecoveryOperation = os.Getenv(operations.EnvRecoveryOperation)
	launchOptions.RecoveryConfiguration = os.Getenv(operations.EnvRecoveryConfiguration)
	if err := launchOptions.Validate(); err != nil {
		return nil, err
	}
	launchOptions.LogLevel = os.Getenv(launchcontract.EnvLogLevel)
	if launchOptions.LogLevel == "" {
		launchOptions.LogLevel = "debug"
	}
	var owner desktopowner.Watcher
	if launchOptions.Mode == launchcontract.ModeDesktop {
		owner, err = desktopowner.Open(launchOptions.DesktopOwnerPID)
		if err != nil {
			return nil, fmt.Errorf("sidraviad: open desktop owner: %w", err)
		}
	}
	packageMode := ""
	if launchOptions.Mode == launchcontract.ModeSystemd {
		packageMode = os.Getenv(launchcontract.EnvSystemdPackage)
		if packageMode != string(productlayout.ModeInstalled) && packageMode != string(productlayout.ModePortable) {
			return nil, errors.New("sidraviad: user service requires a registered package layout")
		}
	}
	paths, err := deriveLaunchPaths(launchOptions.Namespace, packageMode)
	if err != nil {
		if owner != nil {
			_ = owner.Close()
		}
		return nil, err
	}

	if err := configureServiceSwitch(&launchOptions, paths); err != nil {
		if owner != nil {
			_ = owner.Close()
		}
		return nil, err
	}

	onUnprotected := newStorageProtectionWarning(logger)
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{
		IntendedOwnerSID:                   "",
		AllowUnsupportedProtectionFallback: paths.portable,
		OnUnprotected:                      onUnprotected,
		OnPostCommitDiagnostic:             newPostCommitDiagnosticWarning(logger),
	})
	if err != nil {
		if owner != nil {
			_ = owner.Close()
		}
		return nil, fmt.Errorf("sidraviad: create secure store: %w", err)
	}

	hostInfo, err := environment.ReadSystemHostInformation()
	if err != nil {
		if owner != nil {
			_ = owner.Close()
		}
		return nil, fmt.Errorf("sidraviad: read host information: %w", err)
	}

	observer := environment.NewSystemObserver()

	token, err := host.GenerateToken()
	if err != nil {
		if owner != nil {
			_ = owner.Close()
		}
		return nil, fmt.Errorf("sidraviad: generate token: %w", err)
	}

	rt, err := composeObjectGraphWithLaunchOptions(ctx, store, paths, hostInfo, observer, host.Run, token, ProductVersion, BuildID, logger, launchOptions, owner, onUnprotected)
	if err != nil && owner != nil {
		_ = owner.Close()
	}
	return rt, err
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
	return composeObjectGraphWithLaunchOptions(ctx, store, paths, hostInfo, observer, hostRunner, token, productVersion, buildID, logger, launchcontract.Headless(), nil, unprotectedCallbacks...)
}

func composeObjectGraphWithLaunchOptions(
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
	launchOptions launchcontract.Options,
	desktopOwner desktopowner.Watcher,
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
	if err := launchOptions.Validate(); err != nil {
		return nil, fmt.Errorf("sidraviad: launch options: %w", err)
	}
	namespace, err := productlayout.NewNamespace(launchOptions.Namespace)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: runtime namespace: %w", err)
	}
	if launchOptions.Mode == launchcontract.ModeDesktop && desktopOwner == nil {
		return nil, errors.New("sidraviad: desktop owner watcher is required")
	}
	if launchOptions.Mode != launchcontract.ModeDesktop && desktopOwner != nil {
		return nil, errors.New("sidraviad: independent daemon cannot have desktop owner watcher")
	}
	if hostInfo.HostName == "" {
		return nil, errors.New("sidraviad: host name is required")
	}
	var onUnprotected func()
	if len(unprotectedCallbacks) > 0 {
		onUnprotected = unprotectedCallbacks[0]
	}
	for _, entry := range []struct{ name, path string }{
		{"profiles", paths.profiles},
		{"configurations", paths.configurations},
		{"runtime info", paths.runtimeInfo},
	} {
		if entry.path == "" || !filepath.IsAbs(entry.path) || filepath.Clean(entry.path) != entry.path {
			return nil, fmt.Errorf("sidraviad: %s path must be absolute and clean", entry.name)
		}
	}

	registry, err := protocol.NewAuthenticationProtocolRegistry(d520.NewFactory())
	if err != nil {
		return nil, fmt.Errorf("sidraviad: create protocol registry: %w", err)
	}

	// Open the Authentication Configuration catalog before loading institution
	// Profiles. OpenCatalog performs the secure store Read, which prepares and
	// protects the shared configuration root that holds configurations.json.
	// Only after that succeeds may the Profile loader traverse the program-root
	// institution-profiles directory.
	catalog, err := configuration.OpenCatalog(ctx, store, paths.configurations)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: open catalog: %w", err)
	}

	profiles, err := configuration.LoadProfileCatalogFromDirectory(ctx, paths.profiles, registry)
	if err != nil {
		return nil, fmt.Errorf("sidraviad: load profiles: %w", err)
	}

	networkTrace := newNetworkTraceRecorder(logger)
	sup := supervisor.New(supervisor.Dependencies{
		RetryPolicy:                session.NewDefaultRetryPolicy(),
		RetryScheduler:             session.NewTimerRetryScheduler(),
		Diagnostics:                newSessionDiagnostics(logger),
		ProtocolDiagnosticsFactory: protocolDiagnosticsFactoryWithTrace(logger, networkTrace),
	})

	resolver, err := app.NewAuthenticationResolver(catalog, profiles, registry, hostInfo)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create resolver: %w", err))
	}

	application, err := app.NewApplication(catalog, profiles, resolver, sup)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create application: %w", err))
	}

	handler := app.IPCHandler(application, productVersion, buildID, launchOptions)
	stopCh := make(chan struct{}, 1)
	var stopOnce sync.Once
	srv, err := server.NewServer(token, buildID, handler, logger, func(method string) {
		// A committed daemon.stop success response has been written to the
		// client. Signal the runtime-owned stop channel once and
		// non-blockingly; the runtime then cancels its child context and runs
		// the existing graceful shutdown path. The server knows nothing about
		// daemon lifecycle semantics.
		if method == contract.MethodDaemonStop || (method == contract.MethodUpdateCommit && launchOptions.Mode == launchcontract.ModeHeadless) {
			stopOnce.Do(func() { stopCh <- struct{}{} })
		}
	}, application)
	if err != nil {
		return nil, closeAfterCompositionFailure(sup, fmt.Errorf("sidraviad: create ipc server: %w", err))
	}

	hostCfg := host.Config{
		ProductVersion:                     productVersion,
		BuildID:                            buildID,
		Token:                              token,
		Namespace:                          namespace,
		RuntimeInfoPath:                    paths.runtimeInfo,
		Handler:                            http.HandlerFunc(srv.ServeHTTP),
		AllowUnsupportedProtectionFallback: paths.portable,
		OnUnprotected:                      onUnprotected,
		OnPostCommitDiagnostic:             newPostCommitDiagnosticWarning(logger),
	}

	return &composedRuntime{
		observer:       observer,
		snapshotSink:   application,
		automaticLogin: application,
		shutdown:       sup,
		ipcShutdown:    srv,
		hostCfg:        hostCfg,
		hostRunner:     hostRunner,
		logger:         logger,
		productVersion: productVersion,
		buildID:        buildID,
		launchOptions:  launchOptions,
		desktopOwner:   desktopOwner,
		stopCh:         stopCh,
		networkTrace:   networkTrace,
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
