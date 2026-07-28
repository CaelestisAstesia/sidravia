//go:build !windows

package cli

import "testing"

// TestLaunchDaemonProcessUnsupportedOnNonWindows proves non-Windows platforms
// get a fixed Unsupported error instead of attempting Unix daemonization.
func TestLaunchDaemonProcessUnsupportedOnNonWindows(t *testing.T) {
	if err := launchDaemonProcess("info"); err == nil {
		t.Fatal("expected Unsupported error on non-Windows")
	}
}
