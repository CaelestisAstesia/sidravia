//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// launchDaemonProcess starts the sibling sidraviad.exe on Windows with both
// output streams attached to the bounded background log. It returns
// once the process has started; the caller polls for reachability. logLevel
// must be "", "info", "debug" or "trace"; an empty value leaves the child at
// the daemon default. It never changes the parent environment.
func launchDaemonProcess(logLevel string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	daemonPath := filepath.Join(filepath.Dir(exe), "sidraviad.exe")
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("解析 sidraviad 日志目录: %w", err)
	}
	logFile, err := prepareDaemonLog(cacheRoot)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := daemonProcessCommand(daemonPath, logFile, os.Environ(), logLevel)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 sidraviad 子进程: %w", err)
	}
	return nil
}

func daemonProcessCommand(daemonPath string, logFile *os.File, parentEnv []string, logLevel string) *exec.Cmd {
	cmd := exec.Command(daemonPath)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = daemonEnvForLevel(parentEnv, logLevel)
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}
