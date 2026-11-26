//go:build !linux

package installationmanager

import (
	"context"

	productlayout "sidravia/internal/installation/layout"
)

func userService(context.Context, string, productlayout.Layout, func(context.Context, productlayout.Layout) error) (ServiceState, error) {
	return ServiceState{}, ErrServiceUnavailable
}

func cleanupErasedRPM(context.Context) error { return ErrServiceUnavailable }

func pendingServiceSwitch() (ServiceSwitch, bool, error) { return ServiceSwitch{}, false, nil }

func trackRPMAutostart(string, bool) error { return ErrServiceUnavailable }

func recoverAutostartStaging(string) error { return ErrServiceUnavailable }
