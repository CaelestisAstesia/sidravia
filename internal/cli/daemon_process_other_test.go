//go:build !windows && !linux

package cli

import "testing"

// TestLaunchDaemonProcessUnsupportedOnNonWindows proves non-Linux/non-Windows
// platforms get a fixed Unsupported error instead of attempting daemonization.
func TestLaunchDaemonProcessUnsupportedOnNonWindows(t *testing.T) {
	if err := launchDaemonProcess("info"); err == nil {
		t.Fatal("expected Unsupported error on non-Linux/non-Windows")
	}
}
