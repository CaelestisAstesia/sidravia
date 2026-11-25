//go:build !linux && !darwin && !windows

package main

import (
	"fmt"
	"os"
	"runtime"
)

func publishDirectory(_, _ string) error {
	return fmt.Errorf("atomic distribution publication on %s: %w", runtime.GOOS, os.ErrInvalid)
}
