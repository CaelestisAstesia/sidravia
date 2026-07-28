//go:build !windows

package cli

import "errors"

// launchDaemonProcess returns Unsupported on non-Windows platforms. Unix
// daemonization is not implemented in this Campaign; the daemon host itself
// also returns Unsupported on non-Windows.
func launchDaemonProcess(logLevel string) error {
	return errors.New("sidraviad: 仅为 Windows 提供 daemon 进程控制（unsupported）")
}
