//go:build !linux

package serviceswitch

import (
	"context"

	"sidravia/internal/background/daemonctl"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installationmanager"
)

func SwitchService(context.Context, daemonctl.Identity, string, int, bool) (installationmanager.ServiceState, error) {
	return installationmanager.ServiceState{}, installationmanager.ErrServiceUnavailable
}

func replaceService(context.Context, daemonctl.Identity, productlayout.Layout) (installationmanager.ServiceState, error) {
	return installationmanager.ServiceState{}, installationmanager.ErrServiceUnavailable
}
