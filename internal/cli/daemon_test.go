package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"sidravia/internal/clientbootstrap"
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

func testDaemonOperations(result clientbootstrap.ProbeResult, inspectErr error) daemonOperations {
	return daemonOperations{
		inspect: func() (clientbootstrap.ProbeResult, error) { return result, inspectErr },
		connect: func(context.Context, contract.RuntimeInfo) (daemonClient, error) {
			return nil, errors.New("unexpected connect")
		},
		reachable: func(contract.RuntimeInfo) bool { return false },
		start:     func(string) (clientbootstrap.StartOutcome, error) { return clientbootstrap.Started, nil },
	}
}

func TestDaemonStatusPresentationUsesBootstrapInspection(t *testing.T) {
	t.Run("stopped", func(t *testing.T) {
		var output strings.Builder
		if err := runDaemonStatus(testDaemonOperations(clientbootstrap.ProbeResult{State: clientbootstrap.ProbeStopped}, nil), &output); err != nil || output.String() != "守护进程：已停止（stopped）\n" {
			t.Fatalf("err=%v output=%q", err, output.String())
		}
	})
	t.Run("running", func(t *testing.T) {
		var output strings.Builder
		result := clientbootstrap.ProbeResult{State: clientbootstrap.ProbeReachable, Status: &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 7, Status: "running"}}
		if err := runDaemonStatus(testDaemonOperations(result, nil), &output); err != nil || !strings.Contains(output.String(), "守护进程：运行中（running）") {
			t.Fatalf("err=%v output=%q", err, output.String())
		}
	})
	t.Run("unknown", func(t *testing.T) {
		if err := runDaemonStatus(testDaemonOperations(clientbootstrap.ProbeResult{State: clientbootstrap.ProbeMalformed}, nil), &strings.Builder{}); err == nil || err.Error() != daemonUnknownError().Error() {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("incompatible", func(t *testing.T) {
		var output strings.Builder
		result := clientbootstrap.ProbeResult{Info: contract.RuntimeInfo{ProductVersion: "secret-version", BuildID: "secret-build", Token: "secret-token"}, State: clientbootstrap.ProbeIncompatible}
		if err := runDaemonStatus(testDaemonOperations(result, nil), &output); err != nil {
			t.Fatalf("err=%v", err)
		}
		want := "守护进程：当前运行的 daemon 与此 sidraviactl 不属于同一构建。请停止 daemon，或使用与它匹配的完整软件包。\n"
		if output.String() != want || strings.Contains(output.String(), "secret-") {
			t.Fatalf("output=%q", output.String())
		}
	})
}

func TestDaemonStopTargetsContactedGeneration(t *testing.T) {
	info := testRuntimeInfo(100)
	stopped, stopCalls := false, 0
	ops := testDaemonOperations(clientbootstrap.ProbeResult{State: clientbootstrap.ProbeReachable, Info: info, Status: &contract.StatusResult{Mode: "headless"}}, nil)
	ops.connect = func(_ context.Context, got contract.RuntimeInfo) (daemonClient, error) {
		if got.PID != info.PID {
			t.Fatalf("PID=%d", got.PID)
		}
		return &fakeDaemonClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
			if method != contract.MethodDaemonStop {
				t.Fatalf("method=%s", method)
			}
			stopCalls++
			stopped = true
			data, _ := contract.MarshalDaemonStopResult(contract.DaemonStopResult{Status: "stopping"})
			return contract.NewSuccessResponse("1", data), nil
		}}, nil
	}
	ops.reachable = func(got contract.RuntimeInfo) bool { return got.PID == info.PID && !stopped }
	deps := stopDependencies{ops: ops, totalWait: time.Second, pollInterval: time.Millisecond}
	if err := runDaemonStop(deps); err != nil || stopCalls != 1 {
		t.Fatalf("err=%v calls=%d", err, stopCalls)
	}
}

func TestDaemonStopAndRestartRejectNonHeadlessBeforeDispatch(t *testing.T) {
	info := testRuntimeInfo(100)
	for name, status := range map[string]*contract.StatusResult{"nil": nil, "desktop": &contract.StatusResult{Mode: "desktop"}, "unknown": &contract.StatusResult{Mode: "unknown"}, "headless-owner": &contract.StatusResult{Mode: "headless", DesktopOwnerPID: intPointer(42)}} {
		t.Run(name, func(t *testing.T) {
			connects, starts := 0, 0
			ops := testDaemonOperations(clientbootstrap.ProbeResult{State: clientbootstrap.ProbeReachable, Info: info, Status: status}, nil)
			ops.connect = func(context.Context, contract.RuntimeInfo) (daemonClient, error) {
				connects++
				return nil, errors.New("unexpected")
			}
			ops.start = func(string) (clientbootstrap.StartOutcome, error) { starts++; return clientbootstrap.Started, nil }
			stop := stopDependencies{ops: ops, totalWait: time.Second, pollInterval: time.Millisecond}
			if err := runDaemonStop(stop); !errors.Is(err, clientbootstrap.ErrModeConflict) {
				t.Fatalf("stop err=%v", err)
			}
			if err := runDaemonRestart(restartDependencies{ops: ops, stop: stop}, ""); !errors.Is(err, clientbootstrap.ErrModeConflict) {
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
		state clientbootstrap.ProbeState
		want  daemonRestartOutcome
	}{{"stopped", clientbootstrap.ProbeStopped, daemonStartedFromStopped}, {"stale", clientbootstrap.ProbeUnreachable, daemonRestarted}} {
		t.Run(tc.name, func(t *testing.T) {
			starts := 0
			ops := testDaemonOperations(clientbootstrap.ProbeResult{State: tc.state}, nil)
			ops.start = func(string) (clientbootstrap.StartOutcome, error) { starts++; return clientbootstrap.Started, nil }
			deps := restartDependencies{ops: ops, stop: stopDependencies{ops: ops, totalWait: time.Second, pollInterval: time.Millisecond}}
			outcome, err := runDaemonRestartWithOutcome(deps, "debug")
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
