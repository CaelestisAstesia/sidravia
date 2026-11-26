//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/shellintegration"
	"sidravia/internal/installation/userservice"
)

func cleanupErasedRPM(ctx context.Context) error {
	marker := filepath.Join(productlayout.RPMExecutableDirectory, "sidravia.rpm")
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(userservice.ErrServiceConflict, err)
	}
	layout, serviceErr := userservice.CleanupErasedRPM(ctx)
	if _, err := os.Lstat(marker); errors.Is(err, os.ErrNotExist) {
		return errors.Join(serviceErr, shellintegration.CleanupErasedRPM(layout))
	}
	return serviceErr
}
