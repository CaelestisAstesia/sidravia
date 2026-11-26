//go:build !windows

package daemonctl

import (
	"errors"
	"syscall"
)

func connectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
