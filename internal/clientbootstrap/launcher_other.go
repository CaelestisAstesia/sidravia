//go:build !windows && !linux

package clientbootstrap

import (
	"errors"
	"sidravia/internal/launchcontract"
)

// launchDaemonProcess returns Unsupported on non-Linux/non-Windows platforms.
// Unix daemonization is not provided there; the daemon host itself also returns
// Unsupported on those targets.
func launchDaemonProcess(options launchcontract.Options, logLevel string) (daemonLaunch, error) {
	return daemonLaunch{}, errors.New("sidraviad: 仅为 Windows 和 Linux 提供 daemon 进程控制（unsupported）")
}
