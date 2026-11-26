//go:build !linux

package serviceswitch

import (
	"context"
	"sidravia/internal/background/daemonctl"

	"sidravia/internal/installationmanager"
	"sidravia/internal/productlayout"
)

func SwitchService(context.Context, daemonctl.Identity, string, int, bool) (installationmanager.ServiceState, error) {
	return installationmanager.ServiceState{}, installationmanager.ErrServiceUnavailable
}

func replaceService(context.Context, daemonctl.Identity, productlayout.Layout) (installationmanager.ServiceState, error) {
	return installationmanager.ServiceState{}, installationmanager.ErrServiceUnavailable
}
