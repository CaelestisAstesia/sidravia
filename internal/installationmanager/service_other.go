//go:build !linux

package installationmanager

import (
	"context"

	"sidravia/internal/productlayout"
)

func userService(context.Context, string, productlayout.Layout, func(context.Context, productlayout.Layout) error) (ServiceState, error) {
	return ServiceState{}, ErrServiceUnavailable
}

func holdServiceRegistration(context.Context) (context.Context, func(), error) {
	return nil, nil, ErrServiceUnavailable
}

func cleanupErasedRPM(context.Context) error { return ErrServiceUnavailable }

func beginServiceSwitch(context.Context) (context.Context, func(), error) {
	return nil, nil, ErrServiceUnavailable
}
func pendingServiceSwitch() (ServiceSwitch, bool, error) {
	return ServiceSwitch{}, false, nil
}
func saveServiceSwitch(context.Context, *ServiceSwitch) error { return ErrServiceUnavailable }

func reloadServiceSwitch(context.Context) error { return ErrServiceUnavailable }

func trackRPMAutostart(string, bool) error { return ErrServiceUnavailable }

func recoverAutostartStaging(string) error { return ErrServiceUnavailable }
