//go:build windows

package clientbootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"

	"sidravia/internal/productlayout"
)

// launchDaemonProcess starts the sibling sidraviad.exe on Windows with both
// output streams attached to the bounded background log. Process creation is
// not typed readiness: after Start it returns a buffered observation of that
// exact child's Wait result while the caller continues readiness probing.
// logLevel must be "", "info", "debug" or "trace"; an empty value leaves the
// child at the daemon default. It never changes the parent environment.
func launchDaemonProcess(logLevel string) (daemonLaunch, error) {
	return launchDaemonProcessWith(logLevel, defaultWindowsDaemonLauncherDeps())
}

type windowsDaemonLauncherDeps struct {
	resolveLayout func() (productlayout.Layout, error)
	prepareLog    func(string) (*os.File, error)
	parentEnv     func() []string
	start         func(*exec.Cmd) error
	wait          func(*exec.Cmd) error
}

func defaultWindowsDaemonLauncherDeps() windowsDaemonLauncherDeps {
	return windowsDaemonLauncherDeps{productlayout.Resolve, prepareDaemonLog, os.Environ, func(c *exec.Cmd) error { return c.Start() }, func(c *exec.Cmd) error { return c.Wait() }}
}

func launchDaemonProcessWith(logLevel string, deps windowsDaemonLauncherDeps) (daemonLaunch, error) {
	layout, err := deps.resolveLayout()
	if err != nil {
		return daemonLaunch{}, fmt.Errorf("解析 sidravia 运行目录: %w", err)
	}
	daemonPath := filepath.Join(layout.ExecutableDirectory, "sidraviad.exe")
	logFile, err := deps.prepareLog(layout.DaemonLogPath)
	if err != nil {
		return daemonLaunch{}, err
	}
	defer logFile.Close()
	cmd := daemonProcessCommand(daemonPath, logFile, deps.parentEnv(), logLevel)
	if err := deps.start(cmd); err != nil {
		return daemonLaunch{}, fmt.Errorf("启动 sidraviad 子进程: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- deps.wait(cmd) }()
	return daemonLaunch{exited: exited}, nil
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
