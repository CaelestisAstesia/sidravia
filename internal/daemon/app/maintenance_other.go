//go:build !windows && !linux

package app

import "errors"

func openMaintenanceCoordinator(uint32) (func() bool, func(), error) {
	return nil, nil, errors.New("unsupported platform")
}
