package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/ipc/contract"
)

func daemonStatusResponse(t *testing.T) contract.Response {
	t.Helper()
	data, err := contract.MarshalStatusResult(contract.StatusResult{ProductVersion: "1.0.0", BuildID: "b", PID: 42, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewSuccessResponse("1", data)
}

func testDaemonOperations(result daemonctl.ProbeResult, inspectErr error) daemonOperations {
	return daemonOperations{
		inspect: func() (daemonctl.ProbeResult, error) { return result, inspectErr },
		stop:    func(context.Context, contract.RuntimeInfo) error { return errors.New("unexpected stop") },
		start:   func(string) (daemonctl.StartOutcome, error) { return daemonctl.Started, nil },
	}
}

func TestDaemonStatusPresentationUsesBootstrapInspection(t *testing.T) {
	t.Run("stopped", func(t *testing.T) {
		var output strings.Builder
		if err := runDaemonStatus(testDaemonOperations(daemonctl.ProbeResult{State: daemonctl.ProbeStopped}, nil), &output); err != nil || output.String() != "守护进程：已停止（stopped）\n" {
			t.Fatalf("err=%v output=%q", err, output.String())
		}
	})
	t.Run("running", func(t *testing.T) {
		var output strings.Builder
		result := daemonctl.ProbeResult{State: daemonctl.ProbeReachable, Status: &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 7, Status: "running"}}
		if err := runDaemonStatus(testDaemonOperations(result, nil), &output); err != nil || !strings.Contains(output.String(), "守护进程：运行中（running）") {
			t.Fatalf("err=%v output=%q", err, output.String())
		}
	})
	t.Run("unknown", func(t *testing.T) {
		if err := runDaemonStatus(testDaemonOperations(daemonctl.ProbeResult{State: daemonctl.ProbeMalformed}, nil), &strings.Builder{}); err == nil || err.Error() != daemonUnknownError().Error() {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("incompatible", func(t *testing.T) {
		var output strings.Builder
		result := daemonctl.ProbeResult{Info: contract.RuntimeInfo{ProductVersion: "secret-version", BuildID: "secret-build", Token: "secret-token"}, State: daemonctl.ProbeIncompatible}
		if err := runDaemonStatus(testDaemonOperations(result, nil), &output); err != nil {
			t.Fatalf("err=%v", err)
		}
		want := "守护进程：当前运行的 daemon 与此 sidraviactl 不属于同一构建。请停止 daemon，或使用与它匹配的完整软件包。\n"
		if output.String() != want || strings.Contains(output.String(), "secret-") {
			t.Fatalf("output=%q", output.String())
		}
	})
}

func TestRunDaemonStatusSuccessAuthoritative(t *testing.T) {
	result := &contract.StatusResult{
		ProductVersion: "1.4.2",
		BuildID:        "build-deadbeef",
		PID:            9876,
		Status:         "running",
		Mode:           "headless",
	}
	var output strings.Builder
	if err := runDaemonStatus(testDaemonOperations(daemonctl.ProbeResult{State: daemonctl.ProbeReachable, Status: result}, nil), &output); err != nil {
		t.Fatalf("runDaemonStatus = %v, want nil", err)
	}
	expected := fmt.Sprintf("守护进程：运行中（%s） | 版本：%s | 构建：%s | PID：%d | 模式：%s\n",
		result.Status, result.ProductVersion, result.BuildID, result.PID, result.Mode)
	expected += "日志：portable 模式为程序目录下 logs/sidraviad.log；安装版为当前用户缓存目录下 Sidravia/logs/sidraviad.log。\n"
	if got := output.String(); got != expected {
		t.Errorf("runDaemonStatus output = %q, want %q", got, expected)
	}
}

func TestDaemonStopTargetsContactedGeneration(t *testing.T) {
	info := testRuntimeInfo(100)
	stopCalls := 0
	ops := testDaemonOperations(daemonctl.ProbeResult{State: daemonctl.ProbeReachable, Info: info, Status: &contract.StatusResult{Mode: "headless"}}, nil)
	ops.stop = func(_ context.Context, got contract.RuntimeInfo) error {
		if got != info {
			t.Fatalf("wrong selected generation: %+v", got)
		}
		stopCalls++
		return nil
	}
	if err := runDaemonStop(ops); err != nil || stopCalls != 1 {
		t.Fatalf("err=%v calls=%d", err, stopCalls)
	}
}

func TestDaemonStopAndRestartRejectNonHeadlessBeforeDispatch(t *testing.T) {
	info := testRuntimeInfo(100)
	for name, status := range map[string]*contract.StatusResult{"nil": nil, "desktop": &contract.StatusResult{Mode: "desktop"}, "unknown": &contract.StatusResult{Mode: "unknown"}, "headless-owner": &contract.StatusResult{Mode: "headless", DesktopOwnerPID: intPointer(42)}} {
		t.Run(name, func(t *testing.T) {
			connects, starts := 0, 0
			ops := testDaemonOperations(daemonctl.ProbeResult{State: daemonctl.ProbeReachable, Info: info, Status: status}, nil)
			ops.stop = func(context.Context, contract.RuntimeInfo) error {
				connects++
				return errors.New("unexpected")
			}
			ops.start = func(string) (daemonctl.StartOutcome, error) { starts++; return daemonctl.Started, nil }
			if err := runDaemonStop(ops); !errors.Is(err, daemonctl.ErrModeConflict) {
				t.Fatalf("stop err=%v", err)
			}
			if err := runDaemonRestart(ops, ""); !errors.Is(err, daemonctl.ErrModeConflict) {
				t.Fatalf("restart err=%v", err)
			}
			if connects != 0 || starts != 0 {
				t.Fatalf("connects=%d starts=%d", connects, starts)
			}
		})
	}
}

func intPointer(value int) *int { return &value }

func TestDaemonRestartPreservesStoppedAndRunningPolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state daemonctl.ProbeState
		want  daemonRestartOutcome
	}{{"stopped", daemonctl.ProbeStopped, daemonStartedFromStopped}, {"stale", daemonctl.ProbeUnreachable, daemonRestarted}} {
		t.Run(tc.name, func(t *testing.T) {
			starts := 0
			ops := testDaemonOperations(daemonctl.ProbeResult{State: tc.state}, nil)
			ops.start = func(string) (daemonctl.StartOutcome, error) { starts++; return daemonctl.Started, nil }
			outcome, err := runDaemonRestartWithOutcome(ops, "debug")
			if err != nil || outcome != tc.want || starts != 1 {
				t.Fatalf("outcome=%q err=%v starts=%d", outcome, err, starts)
			}
		})
	}
}

func TestParseDaemonLogLevel(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		bad  bool
	}{{nil, "", false}, {[]string{"--log-level", "debug"}, "debug", false}, {[]string{"--log-level", "verbose"}, "", true}} {
		got, err := parseDaemonLogLevel(tc.args)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("args=%v got=%q err=%v", tc.args, got, err)
		}
	}
}
