//go:build !linux

package clientbootstrap

import (
	"context"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installationmanager"
)

func SwitchService(context.Context, Identity, string, int, bool) (installationmanager.ServiceState, error) {
	return installationmanager.ServiceState{}, installationmanager.ErrServiceUnavailable
}

func replaceService(context.Context, Identity, productlayout.Layout) (installationmanager.ServiceState, error) {
	return installationmanager.ServiceState{}, installationmanager.ErrServiceUnavailable
}

func CheckRPMServiceIPC(context.Context, Identity, int) error {
	return installationmanager.ErrServiceUnavailable
}
