package serviceswitch

import (
	"context"
	"os"

	"sidravia/internal/background/daemonctl"
	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/installation/userservice"
	"sidravia/internal/productlayout"
)

// ServiceOperation executes an explicit user action, using the same registration
// that bootstrap reads and the caller's expected IPC generation for readiness.
func ServiceOperation(ctx context.Context, identity daemonctl.Identity, action string) (userservice.ServiceState, error) {
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return userservice.ServiceState{}, err
	}
	layout, err := productlayout.ResolveNamespace(ns)
	if err != nil {
		return userservice.ServiceState{}, err
	}
	if action == "replace" {
		return replaceService(ctx, identity, layout)
	}
	return userservice.Service(ctx, action, layout, func(ctx context.Context, target productlayout.Layout) error {
		return daemonctl.WaitForService(ctx, identity, target)
	})
}
