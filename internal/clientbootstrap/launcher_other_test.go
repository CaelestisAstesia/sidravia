//go:build !windows && !linux

package clientbootstrap

import (
	"sidravia/internal/launchcontract"
	"testing"
)

// TestLaunchDaemonProcessUnsupportedOnNonWindows proves non-Linux/non-Windows
// platforms get a fixed Unsupported error instead of attempting daemonization.
func TestLaunchDaemonProcessUnsupportedOnNonWindows(t *testing.T) {
	launch, err := launchDaemonProcess(launchcontract.Headless(), "info")
	if err == nil {
		t.Fatal("expected Unsupported error on non-Linux/non-Windows")
	}
	if err.Error() != "sidraviad: 仅为 Windows 和 Linux 提供 daemon 进程控制（unsupported）" {
		t.Fatalf("error = %q", err)
	}
	if launch.exited != nil {
		t.Fatalf("exit observation = %v, want nil", launch.exited)
	}
}
