package daemonctl

import (
	"context"
	"os"
	"path/filepath"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/userservice/registration"
)

func serviceLayout(ns productlayout.Namespace) (productlayout.Layout, bool, error) {
	if !ns.IsProduction() {
		return productlayout.Layout{}, false, nil
	}
	layout, registered, err := registration.Lookup()
	if err != nil || !registered {
		return layout, registered, err
	}
	exe, err := os.Executable()
	if err != nil {
		return layout, false, err
	}
	if filepath.Dir(exe) != layout.ExecutableDirectory {
		return layout, false, ErrModeConflict
	}
	return layout, true, nil
}

// ProbeRegisteredService observes the daemon for a specific systemd registration.
func ProbeRegisteredService(ctx context.Context, identity Identity, layout productlayout.Layout) (ProbeResult, error) {
	deps := defaultDependencies()
	deps.runtimeInfoPath = func(productlayout.Namespace) (string, error) { return layout.RuntimeInfoPath, nil }
	result, err := probeInContext(ctx, identity, productlayout.Namespace{}, deps)
	if err != nil {
		return result, err
	}
	if result.State == ProbeReachable && result.Status.Mode != string(launchcontract.ModeSystemd) {
		return ProbeResult{}, ErrModeConflict
	}
	return result, nil
}

func connectService(ctx context.Context, identity Identity, layout productlayout.Layout) (Client, error) {
	result, err := ProbeRegisteredService(ctx, identity, layout)
	if err != nil {
		return nil, err
	}
	if result.State != ProbeReachable {
		return nil, ErrDaemonStopped
	}
	return Connect(ctx, identity, result.Info)
}

// WaitForService confirms the registered unit's IPC, not merely systemd ActiveState.
func WaitForService(ctx context.Context, identity Identity, layout productlayout.Layout) error {
	deps := defaultDependencies()
	ctx, cancel := context.WithTimeout(ctx, deps.totalWait)
	defer cancel()
	return waitForReadinessInContext(ctx, deps.totalWait, deps.pollInterval, nil, func() (bool, error) {
		result, err := ProbeRegisteredService(ctx, identity, layout)
		if err == nil && result.State == ProbeIncompatible {
			err = ErrIncompatibleGeneration
		}
		return result.State == ProbeReachable, err
	}, nil)
}

func bootstrapService(identity Identity, layout productlayout.Layout) (DesktopBootstrapResult, error) {
	result, err := ProbeRegisteredService(context.Background(), identity, layout)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if result.State != ProbeReachable {
		return DesktopBootstrapResult{}, ErrDaemonStopped
	}
	return DesktopBootstrapResult{Info: result.Info, Status: *result.Status}, nil
}

func probeDefault(identity Identity) (ProbeResult, error) {
	deps := defaultDependencies()
	ns, err := deps.namespace()
	if err != nil {
		return ProbeResult{}, err
	}
	layout, registered, err := serviceLayout(ns)
	if err != nil {
		return ProbeResult{}, err
	}
	if registered {
		return ProbeRegisteredService(context.Background(), identity, layout)
	}
	return probe(identity, deps)
}

// HasService reports a matching registration, without starting the daemon.
func HasService() (bool, error) {
	ns, err := defaultDependencies().namespace()
	if err != nil {
		return false, err
	}
	_, registered, err := serviceLayout(ns)
	return registered, err
}
