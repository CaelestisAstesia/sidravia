//go:build windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
)

// launchDaemonProcess starts the sibling sidraviad.exe on Windows, inheriting
// stdout/stderr and setting only the child log-level environment. It returns
// once the process has started; the caller polls for reachability. logLevel
// must be "", "info", "debug" or "trace"; an empty value leaves the child at
// the daemon default. It never changes the parent environment.
func launchDaemonProcess(logLevel string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	daemonPath := filepath.Join(filepath.Dir(exe), "sidraviad.exe")
	cmd := exec.Command(daemonPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = daemonEnvForLevel(os.Environ(), logLevel)
	return cmd.Start()
}
