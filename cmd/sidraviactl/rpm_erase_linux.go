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

func cleanupErasedPackage(ctx context.Context, manager string) error {
	root := productlayout.PackageDirectory(manager)
	if root == "" {
		return userservice.ErrServiceConflict
	}
	if manager == "deb" {
		release, err := admitDebErase(root)
		if err != nil {
			return err
		}
		defer release()
	}
	marker := filepath.Join(root, "sidravia."+manager)
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(userservice.ErrServiceConflict, err)
	}
	layout, serviceErr := userservice.CleanupErasedPackage(ctx, manager)
	if _, err := os.Lstat(marker); errors.Is(err, os.ErrNotExist) {
		return errors.Join(serviceErr, shellintegration.CleanupErasedPackage(layout, manager))
	}
	return serviceErr
}
