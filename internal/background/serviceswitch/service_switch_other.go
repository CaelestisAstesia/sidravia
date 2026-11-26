//go:build !linux

package serviceswitch

import (
	"context"
	"sidravia/internal/background/daemonctl"

	"sidravia/internal/installation/userservice"
	"sidravia/internal/productlayout"
)

func SwitchService(context.Context, daemonctl.Identity, string, int, bool) (userservice.ServiceState, error) {
	return userservice.ServiceState{}, userservice.ErrServiceUnavailable
}

func replaceService(context.Context, daemonctl.Identity, productlayout.Layout) (userservice.ServiceState, error) {
	return userservice.ServiceState{}, userservice.ErrServiceUnavailable
}
