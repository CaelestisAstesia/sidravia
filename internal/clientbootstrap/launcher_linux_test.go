//go:build linux

package clientbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func portableLauncherDeps(t *testing.T, parentEnv []string, start func(*exec.Cmd) error, release func(*exec.Cmd) error) daemonLauncherDeps {
	t.Helper()
	return daemonLauncherDeps{
		resolveLayout: func(productlayout.Namespace) (productlayout.Layout, error) {
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
		resolveLayout: func(productlayout.Namespace) (productlayout.Layout, error) { return productlayout.Layout{}, cause },
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
		resolveLayout: func(productlayout.Namespace) (productlayout.Layout, error) {
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

func TestLinuxRecoveryUsesItsLaunchedProcess(t *testing.T) {
	if os.Getenv("SIDRAVIA_RECOVERY_LAUNCH_FIXTURE") == "1" {
		time.Sleep(30 * time.Second)
		return
	}
	t.Setenv("SIDRAVIA_RECOVERY_LAUNCH_FIXTURE", "1")
	var child *exec.Cmd
	var launched daemonLaunch
	releases := 0
	launcher := portableLauncherDeps(t, os.Environ(), func(cmd *exec.Cmd) error {
		cmd.Path = os.Args[0]
		cmd.Args = []string{os.Args[0], "-test.run=^TestLinuxRecoveryUsesItsLaunchedProcess$"}
		if err := cmd.Start(); err != nil {
			return err
		}
		child = cmd
		return nil
	}, func(*exec.Cmd) error { releases++; return nil })
	waitDone := make(chan struct{})
	launcher.wait = func(cmd *exec.Cmd) error {
		defer close(waitDone)
		return cmd.Wait()
	}
	t.Cleanup(func() {
		if child == nil {
			return
		}
		_ = child.Process.Kill()
		if releases > 0 {
			_ = child.Wait()
			return
		}
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			t.Error("owned child was not reaped")
		}
	})

	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		if child == nil {
			return contract.RuntimeInfo{}, os.ErrNotExist
		}
		return runtime(child.Process.Pid), nil
	}, func(_ context.Context, info contract.RuntimeInfo, _ Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
			return statusResponseValue(t, contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: info.PID, Status: "running", Mode: "headless"}), nil
		}}, nil
	})
	deps.launch = func(options launchcontract.Options, level string) (daemonLaunch, error) {
		var err error
		launched, err = launchDaemonProcessWith(options, level, launcher)
		return launched, err
	}
	info, err := startHeadlessForRecovery(testIdentity(t), "info", deps)
	if err != nil {
		t.Fatalf("recovery could not identify its launched child: %v", err)
	}
	if child == nil || info.PID != child.Process.Pid || launched.alive == nil || releases != 0 {
		t.Fatalf("recovery lost exact process observation: pid=%d releases=%d", info.PID, releases)
	}
	if !slices.Contains(child.Env, launchcontract.EnvSkipAutoLogin+"=1") {
		t.Fatal("recovery launched without suppressing automatic login")
	}
}
