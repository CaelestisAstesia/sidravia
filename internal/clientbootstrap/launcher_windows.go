//go:build windows

package clientbootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"

	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
)

// launchDaemonProcess starts the sibling sidraviad.exe on Windows with both
// output streams attached to the bounded background log. Process creation is
// not typed readiness: after Start it returns a buffered observation of that
// exact child's Wait result while the caller continues readiness probing.
// logLevel must be "", "info", "debug" or "trace"; an empty value leaves the
// child at the daemon default. It never changes the parent environment.
func launchDaemonProcess(options launchcontract.Options, logLevel string) (daemonLaunch, error) {
	return launchDaemonProcessWith(options, logLevel, defaultWindowsDaemonLauncherDeps())
}

type windowsDaemonLauncherDeps struct {
	resolveLayout func(productlayout.Namespace) (productlayout.Layout, error)
	prepareLog    func(string) (*os.File, error)
	parentEnv     func() []string
	start         func(*exec.Cmd) error
	wait          func(*exec.Cmd) error
}

func defaultWindowsDaemonLauncherDeps() windowsDaemonLauncherDeps {
	return windowsDaemonLauncherDeps{productlayout.ResolveNamespace, prepareDaemonLog, os.Environ, func(c *exec.Cmd) error { return c.Start() }, func(c *exec.Cmd) error { return c.Wait() }}
}

func launchDaemonProcessWith(options launchcontract.Options, logLevel string, deps windowsDaemonLauncherDeps) (daemonLaunch, error) {
	if err := options.Validate(); err != nil {
		return daemonLaunch{}, err
	}
	ns, err := productlayout.NewNamespace(options.Namespace)
	if err != nil {
		return daemonLaunch{}, err
	}
	layout, err := deps.resolveLayout(ns)
	if err != nil {
		return daemonLaunch{}, fmt.Errorf("解析 sidravia 运行目录: %w", err)
	}
	daemonPath := filepath.Join(layout.ExecutableDirectory, "sidraviad.exe")
	logFile, err := deps.prepareLog(layout.DaemonLogPath)
	if err != nil {
		return daemonLaunch{}, err
	}
	defer logFile.Close()
	cmd, err := daemonProcessCommand(daemonPath, logFile, deps.parentEnv(), options, logLevel)
	if err != nil {
		return daemonLaunch{}, err
	}
	if err := deps.start(cmd); err != nil {
		return daemonLaunch{}, fmt.Errorf("启动 sidraviad 子进程: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- deps.wait(cmd) }()
	return daemonLaunch{exited: exited}, nil
}

func daemonProcessCommand(daemonPath string, logFile *os.File, parentEnv []string, options launchcontract.Options, logLevel string) (*exec.Cmd, error) {
	cmd := exec.Command(daemonPath)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	var err error
	cmd.Env, err = launchcontract.ChildEnvironment(parentEnv, options, logLevel)
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd, nil
}
