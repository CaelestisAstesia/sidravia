//go:build !linux

package daemonctl

import (
	"context"

	"sidravia/internal/installation/userservice/registration"
)

func CheckPackageServiceIPC(context.Context, Identity, int, string) error {
	return registration.ErrUnavailable
}
