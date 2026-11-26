//go:build !windows && !linux

package application

import "errors"

func openMaintenanceCoordinator(uint32) (func() bool, func(), error) {
	return nil, nil, errors.New("unsupported platform")
}
