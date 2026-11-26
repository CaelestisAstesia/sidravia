//go:build linux

package daemonctl

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/background/logging"
	productlayout "sidravia/internal/installation/layout"
)

// daemonSiblingName is the exact sibling executable name the CLI launches.
const daemonSiblingName = "sidraviad"

func launchDaemonProcess(options launchcontract.Options, logLevel string) (daemonLaunch, error) {
	return launchDaemonProcessWith(options, logLevel, defaultDaemonLauncherDeps())
}

// daemonLauncherDeps bundles the operations launchDaemonProcessWith depends on,
// so tests can inject private replacements for layout resolution, log
// preparation, environment, start and release. It is not an exported seam.
type daemonLauncherDeps struct {
	resolveLayout func(productlayout.Namespace) (productlayout.Layout, error)
	prepareLog    func(string) (*os.File, error)
	parentEnv     func() []string
	start         func(*exec.Cmd) error
	release       func(*exec.Cmd) error
	wait          func(*exec.Cmd) error
}

func defaultDaemonLauncherDeps() daemonLauncherDeps {
	return daemonLauncherDeps{
		resolveLayout: productlayout.ResolveNamespace,
		prepareLog:    logging.PrepareBootstrap,
		parentEnv:     os.Environ,
		start:         func(cmd *exec.Cmd) error { return cmd.Start() },
		wait:          func(cmd *exec.Cmd) error { return cmd.Wait() },
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
// and both output streams owned by the log file, and detaches it into its own
// session. Ordinary headless launches release the process; desktop and recovery
// launches own a Wait observation. Only the parent's log handle is closed here.
// Operation failures preserve private causes behind fixed safe labels.
func launchDaemonProcessWith(options launchcontract.Options, logLevel string, deps daemonLauncherDeps) (daemonLaunch, error) {
	if err := options.Validate(); err != nil {
		return daemonLaunch{}, err
	}
	ns, err := productlayout.NewNamespace(options.Namespace)
	if err != nil {
		return daemonLaunch{}, err
	}
	layout, err := deps.resolveLayout(ns)
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
	cmd.Env, err = launchcontract.ChildEnvironment(deps.parentEnv(), options, logLevel)
	if err != nil {
		_ = logFile.Close()
		return daemonLaunch{}, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := deps.start(cmd); err != nil {
		_ = logFile.Close()
		return daemonLaunch{}, wrapSafeOperation("启动 sidraviad", err)
	}

	// Desktop bootstrap observes early exit/singleton contention. Recovery of a
	// headless generation also pins the exact child until readiness is checked;
	// ordinary headless callers retain their detached lifetime.
	if options.Mode == launchcontract.ModeDesktop || options.SkipAutoLogin {
		launch := daemonLaunch{}
		if cmd.Process != nil {
			launch.pid = cmd.Process.Pid
		}
		var observationErr error
		if options.Mode == launchcontract.ModeHeadless {
			if launch.pid <= 0 {
				observationErr = errors.New("launched process identity is unavailable")
			} else {
				var fd int
				fd, observationErr = unix.PidfdOpen(launch.pid, 0)
				if observationErr == nil {
					launch.alive = func() bool {
						fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
						_, err := unix.Poll(fds, 0)
						return err == nil && fds[0].Revents == 0
					}
					launch.release = func() { _ = unix.Close(fd) }
				}
			}
		}
		exited := make(chan error, 1)
		go func() {
			err := deps.wait(cmd)
			var status *exec.ExitError
			if errors.As(err, &status) && status.ExitCode() == launchcontract.ExitSingletonContention {
				err = errors.Join(errDaemonSingletonContention, err)
			}
			exited <- err
		}()
		closeErr := logFile.Close()
		if observationErr != nil || closeErr != nil {
			if launch.release != nil {
				launch.release()
			}
			return daemonLaunch{}, wrapSafeOperation("观察 sidraviad 启动", errors.Join(observationErr, closeErr))
		}
		launch.exited = exited
		return launch, nil
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
