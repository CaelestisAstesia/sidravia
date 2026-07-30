//go:build linux

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"sidravia/internal/productlayout"
)

// daemonSiblingName is the exact sibling executable name the CLI launches.
const daemonSiblingName = "sidraviad"

func launchDaemonProcess(logLevel string) (daemonLaunch, error) {
	return launchDaemonProcessWith(logLevel, defaultDaemonLauncherDeps())
}

// daemonLauncherDeps bundles the operations launchDaemonProcessWith depends on,
// so tests can inject private replacements for layout resolution, log
// preparation, environment, start and release. It is not an exported seam.
type daemonLauncherDeps struct {
	resolveLayout func() (productlayout.Layout, error)
	prepareLog    func(string) (*os.File, error)
	parentEnv     func() []string
	start         func(*exec.Cmd) error
	release       func(*exec.Cmd) error
}

func defaultDaemonLauncherDeps() daemonLauncherDeps {
	return daemonLauncherDeps{
		resolveLayout: productlayout.Resolve,
		prepareLog:    prepareDaemonLog,
		parentEnv:     os.Environ,
		start:         func(cmd *exec.Cmd) error { return cmd.Start() },
		release: func(cmd *exec.Cmd) error {
			if cmd.Process == nil {
				return nil
			}
			return cmd.Process.Release()
		},
	}
}

// launchDaemonProcessWith resolves the shared product layout, selects the exact
// sibling sidraviad, prepares the bounded log, starts the child with nil stdin
// and both output streams owned by the log file, detaches it into its own
// session, releases the process because the CLI does not own a later Wait, and
// closes only the parent's log handle. Resolve, log, start and release failures
// are wrapped with fixed safe Chinese operation labels while preserving causes.
func launchDaemonProcessWith(logLevel string, deps daemonLauncherDeps) (daemonLaunch, error) {
	layout, err := deps.resolveLayout()
	if err != nil {
		return daemonLaunch{}, wrapSafeOperation("解析 sidraviad 运行目录", err)
	}
	daemonPath := filepath.Join(layout.ExecutableDirectory, daemonSiblingName)

	logFile, err := deps.prepareLog(layout.DaemonLogPath)
	if err != nil {
		return daemonLaunch{}, wrapSafeOperation("准备 sidraviad 日志", err)
	}

	cmd := exec.Command(daemonPath)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = daemonEnvForLevel(deps.parentEnv(), logLevel)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := deps.start(cmd); err != nil {
		_ = logFile.Close()
		return daemonLaunch{}, wrapSafeOperation("启动 sidraviad", err)
	}

	if err := deps.release(cmd); err != nil {
		_ = logFile.Close()
		return daemonLaunch{}, wrapSafeOperation("释放 sidraviad 进程", err)
	}

	if err := logFile.Close(); err != nil {
		return daemonLaunch{}, err
	}
	return daemonLaunch{}, nil
}
