//go:build !linux

package userservice

import (
	"context"

	productlayout "sidravia/internal/installation/layout"
)

func userService(context.Context, string, productlayout.Layout, func(context.Context, productlayout.Layout) error) (ServiceState, error) {
	return ServiceState{}, ErrServiceUnavailable
}

func CleanupErasedRPM(context.Context) (productlayout.Layout, error) {
	return productlayout.Layout{}, ErrServiceUnavailable
}

func pendingServiceSwitch() (ServiceSwitch, bool, error) { return ServiceSwitch{}, false, nil }
