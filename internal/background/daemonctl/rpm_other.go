//go:build !linux

package daemonctl

import (
	"context"

	"sidravia/internal/installation/userservice/registration"
)

func CheckRPMServiceIPC(context.Context, Identity, int) error { return registration.ErrUnavailable }
