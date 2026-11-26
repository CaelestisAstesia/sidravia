package daemonctl

import (
	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/productlayout"
)

// DesktopBootstrapResult is authoritative only after desktop readiness has
// confirmed the exact requested owner and build identity.
type DesktopBootstrapResult struct {
	Info   contract.RuntimeInfo
	Status contract.StatusResult
}

func BootstrapDesktop(identity Identity, ownerPID int) (DesktopBootstrapResult, error) {
	deps := defaultDependencies()
	ns, err := deps.namespace()
	if err != nil {
		return DesktopBootstrapResult{}, wrapSafeOperation("运行命名空间", err)
	}
	if layout, registered, err := serviceLayout(ns); registered || err != nil {
		if err != nil {
			return DesktopBootstrapResult{}, err
		}
		return bootstrapService(identity, layout)
	}

	return bootstrapDesktopWithNamespace(identity, ownerPID, ns, deps)
}

// BootstrapManagedDesktop is read-only, even if systemd registration has
// disappeared since the GUI observed it.
func BootstrapManagedDesktop(identity Identity, _ int) (DesktopBootstrapResult, error) {
	ns, err := defaultDependencies().namespace()
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	layout, registered, err := serviceLayout(ns)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if !registered {
		return DesktopBootstrapResult{}, ErrDaemonStopped
	}
	return bootstrapService(identity, layout)
}

func bootstrapDesktopWithDependencies(identity Identity, ownerPID int, deps dependencies) (DesktopBootstrapResult, error) {
	ns, err := deps.namespace()
	if err != nil {
		return DesktopBootstrapResult{}, wrapSafeOperation("运行命名空间", err)
	}
	return bootstrapDesktopWithNamespace(identity, ownerPID, ns, deps)
}

func bootstrapDesktopWithNamespace(identity Identity, ownerPID int, ns productlayout.Namespace, deps dependencies) (DesktopBootstrapResult, error) {
	options, err := desktopOptions(ownerPID, ns)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if !desktopBootstrapSupported() {
		return DesktopBootstrapResult{}, ErrDesktopUnsupported
	}
	return bootstrapDesktop(identity, options, deps)
}

func desktopOptionsWithNamespace(ownerPID int, deps dependencies) (launchcontract.Options, error) {
	ns, err := deps.namespace()
	if err != nil {
		return launchcontract.Options{}, wrapSafeOperation("运行命名空间", err)
	}
	return desktopOptions(ownerPID, ns)
}

func desktopOptions(ownerPID int, ns productlayout.Namespace) (launchcontract.Options, error) {
	options, err := launchcontract.New(launchcontract.ModeDesktop, ownerPID, ns.String())
	if err != nil {
		return launchcontract.Options{}, err
	}
	return options, nil
}

func bootstrapDesktop(identity Identity, options launchcontract.Options, deps dependencies) (DesktopBootstrapResult, error) {
	ns, err := productlayout.NewNamespace(options.Namespace)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if _, err := ensureWithNamespace(identity, options, options.LogLevel, ns, deps); err != nil {
		return DesktopBootstrapResult{}, err
	}
	result, err := probeWithNamespace(identity, ns, deps)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if result.State != ProbeReachable || !matchesOptions(result.Status, options) ||
		result.Status.ProductVersion != identity.ProductVersion || result.Status.BuildID != identity.BuildID || result.Status.PID <= 0 {
		return DesktopBootstrapResult{}, ErrModeConflict
	}
	return DesktopBootstrapResult{Info: result.Info, Status: *result.Status}, nil
}

// BootstrapDesktopWithLogLevel starts a desktop generation with explicit launch policy.
// Hot generations are retained; the returned status owns the actual log level.
func BootstrapDesktopWithLogLevel(identity Identity, ownerPID int, level string, skipAutoLogin bool) (DesktopBootstrapResult, error) {
	return bootstrapDesktopPolicy(identity, ownerPID, level, skipAutoLogin, "", "", "")
}

// BootstrapDesktopForUpdate delegates only the initial, authorized cold launch.
// Ordinary bootstrap/restart never inherits recovery from ambient environment.
func BootstrapDesktopForUpdate(identity Identity, ownerPID int, level, operation, token, configuration string) (DesktopBootstrapResult, error) {
	return bootstrapDesktopPolicy(identity, ownerPID, level, true, operation, token, configuration)
}
func bootstrapDesktopPolicy(identity Identity, ownerPID int, level string, skipAutoLogin bool, operation, token, configuration string) (DesktopBootstrapResult, error) {
	if level != "info" && level != "debug" && level != "trace" {
		return DesktopBootstrapResult{}, launchcontract.ErrInvalidOptions
	}
	deps := defaultDependencies()
	ns, err := deps.namespace()
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if layout, registered, err := serviceLayout(ns); registered || err != nil {
		if err != nil {
			return DesktopBootstrapResult{}, err
		}
		return bootstrapService(identity, layout)
	}

	options, err := desktopOptions(ownerPID, ns)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if !desktopBootstrapSupported() {
		return DesktopBootstrapResult{}, ErrDesktopUnsupported
	}
	options.LogLevel = level
	options.SkipAutoLogin = skipAutoLogin
	options.RecoveryOperation = operation
	options.RecoveryToken = token
	options.RecoveryConfiguration = configuration
	if err := options.Validate(); err != nil {
		return DesktopBootstrapResult{}, err
	}
	return bootstrapDesktop(identity, options, deps)
}
