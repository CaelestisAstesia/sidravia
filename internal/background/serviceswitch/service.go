package serviceswitch

import (
	"context"
	"os"

	"sidravia/internal/background/daemonctl"
	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installationmanager"
)

// ServiceOperation executes an explicit user action, using the same registration
// that bootstrap reads and the caller's expected IPC generation for readiness.
func ServiceOperation(ctx context.Context, identity daemonctl.Identity, action string) (installationmanager.ServiceState, error) {
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return installationmanager.ServiceState{}, err
	}
	layout, err := productlayout.ResolveNamespace(ns)
	if err != nil {
		return installationmanager.ServiceState{}, err
	}
	if action == "replace" {
		return replaceService(ctx, identity, layout)
	}
	return installationmanager.Service(ctx, action, layout, func(ctx context.Context, target productlayout.Layout) error {
		return daemonctl.WaitForService(ctx, identity, target)
	})
}
