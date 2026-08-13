//go:build linux

package clientbootstrap

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
)

func portableLauncherDeps(t *testing.T, parentEnv []string, start func(*exec.Cmd) error, release func(*exec.Cmd) error) daemonLauncherDeps {
	t.Helper()
	return daemonLauncherDeps{
		resolveLayout: func() (productlayout.Layout, error) {
			return productlayout.Layout{
				Mode:                productlayout.ModePortable,
				ExecutableDirectory: "/opt/sidravia",
				DaemonLogPath:       "/opt/sidravia/logs/sidraviad.log",
			}, nil
		},
		prepareLog: func(string) (*os.File, error) {
			return os.CreateTemp(t.TempDir(), "sidraviad-log-*")
		},
		parentEnv: func() []string { return parentEnv },
		start:     start,
		release:   release,
	}
}

func TestLaunchDaemonProcessLinuxBuildsCommand(t *testing.T) {
	var capturedCmd *exec.Cmd
	var logFile *os.File
	parentEnv := []string{"HOME=/home/user", "SIDRAVIA_LOG_LEVEL=trace", "PATH=/usr/bin"}
	deps := portableLauncherDeps(t, parentEnv,
		func(cmd *exec.Cmd) error {
			capturedCmd = cmd
			return nil
		},
		func(*exec.Cmd) error { return nil },
	)
	deps.prepareLog = func(string) (*os.File, error) {
		f, err := os.CreateTemp(t.TempDir(), "sidraviad-log-*")
		if err != nil {
			return nil, err
		}
		logFile = f
		return f, nil
	}

	launch, err := launchDaemonProcessWith(launchcontract.Headless(), "debug", deps)
	if err != nil {
		t.Fatalf("launchDaemonProcessWith = %v, want nil", err)
	}
	if launch.exited != nil {
		t.Fatalf("exit observation = %v, want nil", launch.exited)
	}

	if capturedCmd.Path != "/opt/sidravia/sidraviad" {
		t.Errorf("cmd.Path = %q, want /opt/sidravia/sidraviad", capturedCmd.Path)
	}
	if capturedCmd.Stdin != nil {
		t.Error("cmd.Stdin = non-nil, want nil")
	}
	if capturedCmd.Stdout != logFile || capturedCmd.Stderr != logFile {
		t.Error("cmd.Stdout/Stderr not shared with log file")
	}
	if capturedCmd.SysProcAttr == nil || !capturedCmd.SysProcAttr.Setsid {
		t.Error("cmd.SysProcAttr.Setsid = false, want detached session")
	}
	wantEnv, err := launchcontract.ChildEnvironment(parentEnv, launchcontract.Headless(), "debug")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(capturedCmd.Env, wantEnv) {
		t.Errorf("cmd.Env = %v, want %v", capturedCmd.Env, wantEnv)
	}
	if slices.Contains(capturedCmd.Env, "SIDRAVIA_LOG_LEVEL=trace") {
		t.Error("cmd.Env retains parent SIDRAVIA_LOG_LEVEL=trace")
	}
}

func TestLaunchDaemonProcessLinuxResolveErrorOwnership(t *testing.T) {
	cause := errors.New("injected resolve failure")
	deps := daemonLauncherDeps{
		resolveLayout: func() (productlayout.Layout, error) { return productlayout.Layout{}, cause },
		prepareLog:    func(string) (*os.File, error) { return os.CreateTemp(t.TempDir(), "log-*") },
		parentEnv:     func() []string { return nil },
		start:         func(*exec.Cmd) error { return nil },
		release:       func(*exec.Cmd) error { return nil },
	}
	launch, err := launchDaemonProcessWith(launchcontract.Headless(), "info", deps)
	if launch.exited != nil {
		t.Errorf("exit observation = %v, want nil", launch.exited)
	}
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want resolve cause", err)
	}
	if !strings.Contains(err.Error(), "解析 sidraviad 运行目录") {
		t.Errorf("error = %v, want resolve label", err)
	}
}

func TestLaunchDaemonProcessLinuxLogErrorOwnership(t *testing.T) {
	cause := errors.New("injected log failure")
	deps := daemonLauncherDeps{
		resolveLayout: func() (productlayout.Layout, error) {
			return productlayout.Layout{ExecutableDirectory: "/opt/sidravia", DaemonLogPath: "/opt/sidravia/logs/sidraviad.log"}, nil
		},
		prepareLog: func(string) (*os.File, error) { return nil, cause },
		parentEnv:  func() []string { return nil },
		start:      func(*exec.Cmd) error { return nil },
		release:    func(*exec.Cmd) error { return nil },
	}
	launch, err := launchDaemonProcessWith(launchcontract.Headless(), "info", deps)
	if launch.exited != nil {
		t.Errorf("exit observation = %v, want nil", launch.exited)
	}
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want log cause", err)
	}
	if !strings.Contains(err.Error(), "准备 sidraviad 日志") {
		t.Errorf("error = %v, want log label", err)
	}
}

func TestLaunchDaemonProcessLinuxStartErrorOwnership(t *testing.T) {
	cause := errors.New("injected start failure")
	deps := portableLauncherDeps(t, nil,
		func(*exec.Cmd) error { return cause },
		func(*exec.Cmd) error { return nil },
	)
	launch, err := launchDaemonProcessWith(launchcontract.Headless(), "info", deps)
	if launch.exited != nil {
		t.Errorf("exit observation = %v, want nil", launch.exited)
	}
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want start cause", err)
	}
	if !strings.Contains(err.Error(), "启动 sidraviad") {
		t.Errorf("error = %v, want start label", err)
	}
}

func TestLaunchDaemonProcessLinuxReleaseErrorOwnership(t *testing.T) {
	cause := errors.New("injected release failure")
	deps := portableLauncherDeps(t, nil,
		func(*exec.Cmd) error { return nil },
		func(*exec.Cmd) error { return cause },
	)
	launch, err := launchDaemonProcessWith(launchcontract.Headless(), "info", deps)
	if launch.exited != nil {
		t.Errorf("exit observation = %v, want nil", launch.exited)
	}
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want release cause", err)
	}
	if !strings.Contains(err.Error(), "释放 sidraviad 进程") {
		t.Errorf("error = %v, want release label", err)
	}
}
