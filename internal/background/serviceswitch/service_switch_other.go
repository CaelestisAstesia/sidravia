//go:build !linux

package serviceswitch

import (
	"context"
	"sidravia/internal/background/daemonctl"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/userservice"
)

func SwitchService(context.Context, daemonctl.Identity, string, int, bool) (userservice.ServiceState, error) {
	return userservice.ServiceState{}, userservice.ErrServiceUnavailable
}

func replaceService(context.Context, daemonctl.Identity, productlayout.Layout) (userservice.ServiceState, error) {
	return userservice.ServiceState{}, userservice.ErrServiceUnavailable
}
