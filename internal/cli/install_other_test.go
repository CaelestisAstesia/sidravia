//go:build !windows

package cli

import (
	"strings"
	"testing"
)

// TestInstallUnsupported proves the Windows-only integration commands return a
// clear Unsupported result on non-Windows platforms without touching any
// system seam.
func TestInstallUnsupported(t *testing.T) {
	if err := runInstallCommand("info"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("runInstallCommand = %v, want unsupported", err)
	}
	if err := runUninstallCommand(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("runUninstallCommand = %v, want unsupported", err)
	}
}
