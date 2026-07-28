package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

func fakeProbeDeps(state probeState, info contract.RuntimeInfo, call func(string, json.RawMessage) (contract.Response, error)) probeDependencies {
	return probeDependencies{
		runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
		readRuntimeInfo: func(string) (contract.RuntimeInfo, error) {
			switch state {
			case probeStopped:
				return contract.RuntimeInfo{}, os.ErrNotExist
			case probeMalformed:
				return contract.RuntimeInfo{}, errors.New("malformed runtime info")
			default:
				return info, nil
			}
		},
		connect: func(ctx context.Context, _ contract.RuntimeInfo) (daemonClient, error) {
			if state == probeUnreachable {
				return nil, errors.New("injected unreachable")
			}
			return &fakeDaemonClient{call: call}, nil
		},
		callTimeout: time.Second,
	}
}

func daemonStatusResponse(t *testing.T) contract.Response {
	t.Helper()
	data, err := contract.MarshalStatusResult(contract.StatusResult{
		ProductVersion: "1.0.0", BuildID: "b", PID: 42, Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewSuccessResponse("1", data)
}

func TestDaemonStatusMissingPrintsStopped(t *testing.T) {
	deps := fakeProbeDeps(probeStopped, contract.RuntimeInfo{}, nil)
	var buf strings.Builder
	if err := runDaemonStatus(deps, &buf); err != nil {
		t.Fatalf("runDaemonStatus: %v", err)
	}
	if buf.String() != "守护进程：已停止（stopped）\n" {
		t.Errorf("output = %q, want stopped", buf.String())
	}
}

func TestDaemonStatusReachablePrintsRunning(t *testing.T) {
	info := testRuntimeInfo(100)
	deps := fakeProbeDeps(probeReachable, info, func(method string, _ json.RawMessage) (contract.Response, error) {
		if method != contract.MethodDaemonStatus {
			t.Errorf("method = %q, want daemon.status", method)
		}
		return daemonStatusResponse(t), nil
	})
	var buf strings.Builder
	if err := runDaemonStatus(deps, &buf); err != nil {
		t.Fatalf("runDaemonStatus: %v", err)
	}
	if !strings.Contains(buf.String(), "守护进程：运行中（running）") {
		t.Errorf("output = %q, want running", buf.String())
	}
}

func TestDaemonStatusUnreachableReturnsError(t *testing.T) {
	info := testRuntimeInfo(100)
	deps := fakeProbeDeps(probeUnreachable, info, nil)
	var buf strings.Builder
	err := runDaemonStatus(deps, &buf)
	if err == nil {
		t.Fatal("expected unreachable error")
	}
	if err.Error() != "守护进程：状态未知（unreachable）" {
		t.Errorf("err = %q", err.Error())
	}
	if buf.Len() != 0 {
		t.Errorf("unreachable produced output %q", buf.String())
	}
}

func TestDaemonStatusMalformedReturnsError(t *testing.T) {
	deps := fakeProbeDeps(probeMalformed, contract.RuntimeInfo{}, nil)
	var buf strings.Builder
	err := runDaemonStatus(deps, &buf)
	if err == nil {
		t.Fatal("expected malformed error")
	}
	if err.Error() != "守护进程：状态未知（unreachable）" {
		t.Errorf("err = %q", err.Error())
	}
	if buf.Len() != 0 {
		t.Errorf("malformed produced output %q", buf.String())
	}
}

func TestProbeDependenciesCannotLaunchDaemon(t *testing.T) {
	typ := reflect.TypeOf(probeDependencies{})
	if _, ok := typ.FieldByName("launch"); ok {
		t.Fatal("read-only probe dependencies expose launch callback")
	}
	if _, ok := typ.FieldByName("startDaemon"); ok {
		t.Fatal("read-only probe dependencies expose start callback")
	}
}

func TestDaemonStartIdempotentWhenReachable(t *testing.T) {
	info := testRuntimeInfo(100)
	launches := 0
	deps := ensureDependencies{
		probe: fakeProbeDeps(probeReachable, info, func(string, json.RawMessage) (contract.Response, error) {
			return daemonStatusResponse(t), nil
		}),
		launch:       func(string) error { launches++; return nil },
		logLevel:     "",
		totalWait:    time.Second,
		pollInterval: time.Millisecond,
	}
	if err := ensureDaemonRunning(deps); err != nil {
		t.Fatalf("ensureDaemonRunning: %v", err)
	}
	if launches != 0 {
		t.Errorf("launches = %d, want 0 (idempotent)", launches)
	}
}

func TestDaemonStartLaunchesWhenStopped(t *testing.T) {
	reachable := false
	info := testRuntimeInfo(100)
	launches := 0
	deps := ensureDependencies{
		probe: probeDependencies{
			runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
			readRuntimeInfo: func(string) (contract.RuntimeInfo, error) {
				if !reachable {
					return contract.RuntimeInfo{}, os.ErrNotExist
				}
				return info, nil
			},
			connect: func(ctx context.Context, _ contract.RuntimeInfo) (daemonClient, error) {
				return &fakeDaemonClient{call: func(string, json.RawMessage) (contract.Response, error) {
					return daemonStatusResponse(t), nil
				}}, nil
			},
			callTimeout: time.Second,
		},
		launch: func(string) error {
			launches++
			reachable = true
			return nil
		},
		logLevel:     "debug",
		totalWait:    time.Second,
		pollInterval: time.Millisecond,
	}
	if err := ensureDaemonRunning(deps); err != nil {
		t.Fatalf("ensureDaemonRunning: %v", err)
	}
	if launches != 1 {
		t.Errorf("launches = %d, want 1", launches)
	}
}

func TestDaemonStopIdempotentWhenNoDaemon(t *testing.T) {
	deps := stopDependencies{
		probe:        fakeProbeDeps(probeStopped, contract.RuntimeInfo{}, nil),
		totalWait:    time.Second,
		pollInterval: time.Millisecond,
	}
	if err := runDaemonStop(deps); err != nil {
		t.Fatalf("runDaemonStop: %v", err)
	}
}

func TestDaemonStopRejectsMalformedAndUnreachableRuntimeInfo(t *testing.T) {
	for _, state := range []probeState{probeMalformed, probeUnreachable} {
		deps := stopDependencies{
			probe:        fakeProbeDeps(state, testRuntimeInfo(100), nil),
			totalWait:    time.Second,
			pollInterval: time.Millisecond,
		}
		if err := runDaemonStop(deps); err == nil || err.Error() != "守护进程：状态未知（unreachable）" {
			t.Errorf("state %d error = %v, want fixed unreachable error", state, err)
		}
	}
}

func TestDaemonStopSendsStopAndWaitsForUnreachable(t *testing.T) {
	info := testRuntimeInfo(100)
	reachable := true
	stopCalls := 0
	call := func(method string, _ json.RawMessage) (contract.Response, error) {
		switch method {
		case contract.MethodDaemonStatus:
			if !reachable {
				return contract.Response{}, errors.New("unreachable")
			}
			return daemonStatusResponse(t), nil
		case contract.MethodDaemonStop:
			stopCalls++
			reachable = false
			data, _ := contract.MarshalDaemonStopResult(contract.DaemonStopResult{Status: "stopping"})
			return contract.NewSuccessResponse("1", data), nil
		default:
			return contract.Response{}, errors.New("unexpected method")
		}
	}
	deps := stopDependencies{
		probe: probeDependencies{
			runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
			readRuntimeInfo: func(string) (contract.RuntimeInfo, error) { return info, nil },
			connect: func(ctx context.Context, _ contract.RuntimeInfo) (daemonClient, error) {
				return &fakeDaemonClient{call: call}, nil
			},
			callTimeout: time.Second,
		},
		totalWait:    time.Second,
		pollInterval: time.Millisecond,
	}
	if err := runDaemonStop(deps); err != nil {
		t.Fatalf("runDaemonStop: %v", err)
	}
	if stopCalls != 1 {
		t.Errorf("daemon.stop called %d times, want 1", stopCalls)
	}
}

func TestDaemonRestartStoppedIsStart(t *testing.T) {
	reachable := false
	info := testRuntimeInfo(100)
	launches := 0
	stopCalls := 0
	probe := probeDependencies{
		runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
		readRuntimeInfo: func(string) (contract.RuntimeInfo, error) {
			if !reachable {
				return contract.RuntimeInfo{}, os.ErrNotExist
			}
			return info, nil
		},
		connect: func(ctx context.Context, _ contract.RuntimeInfo) (daemonClient, error) {
			return &fakeDaemonClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
				if method == contract.MethodDaemonStop {
					stopCalls++
				}
				return daemonStatusResponse(t), nil
			}}, nil
		},
		callTimeout: time.Second,
	}
	deps := restartDependencies{
		probe: probe,
		stop: stopDependencies{
			probe:        probe,
			totalWait:    time.Second,
			pollInterval: time.Millisecond,
		},
		ensure: ensureDependencies{
			probe: probe,
			launch: func(string) error {
				launches++
				reachable = true
				return nil
			},
			totalWait:    time.Second,
			pollInterval: time.Millisecond,
		},
	}
	if err := runDaemonRestart(deps); err != nil {
		t.Fatalf("runDaemonRestart: %v", err)
	}
	if launches != 1 {
		t.Errorf("launches = %d, want 1", launches)
	}
	if stopCalls != 0 {
		t.Errorf("stop calls = %d, want 0 (stopped restarts without stop)", stopCalls)
	}
}

func TestDaemonRestartStopsOriginalGeneration(t *testing.T) {
	generationA := testRuntimeInfo(100)
	generationB := testRuntimeInfo(200)
	current := generationA
	reachableA := true
	var stopTargets []int
	probe := probeDependencies{
		runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
		readRuntimeInfo: func(string) (contract.RuntimeInfo, error) { return current, nil },
		connect: func(_ context.Context, info contract.RuntimeInfo) (daemonClient, error) {
			if info.PID == generationA.PID && !reachableA {
				return nil, errors.New("generation A stopped")
			}
			return &fakeDaemonClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
				if method == contract.MethodDaemonStop {
					stopTargets = append(stopTargets, info.PID)
					reachableA = false
					current = generationB
					data, _ := contract.MarshalDaemonStopResult(contract.DaemonStopResult{Status: "stopping"})
					return contract.NewSuccessResponse("1", data), nil
				}
				return daemonStatusResponse(t), nil
			}}, nil
		},
		callTimeout: time.Second,
	}
	deps := restartDependencies{
		probe: probe,
		stop: stopDependencies{
			probe:        probe,
			totalWait:    time.Second,
			pollInterval: time.Millisecond,
		},
		ensure: ensureDependencies{
			probe:        probe,
			launch:       func(string) error { return errors.New("must not launch") },
			totalWait:    time.Second,
			pollInterval: time.Millisecond,
		},
	}
	if err := runDaemonRestart(deps); err != nil {
		t.Fatalf("runDaemonRestart: %v", err)
	}
	if len(stopTargets) != 1 || stopTargets[0] != generationA.PID {
		t.Errorf("stop targets = %v, want [%d]", stopTargets, generationA.PID)
	}
}

func TestEnsureDaemonRunningPropagatesProbePathError(t *testing.T) {
	launches := 0
	deps := ensureDependencies{
		probe: probeDependencies{
			runtimeInfoPath: func() (string, error) { return "", errors.New("path failure") },
		},
		launch: func(string) error { launches++; return nil },
	}
	if err := ensureDaemonRunning(deps); err == nil {
		t.Fatal("expected probe path error")
	}
	if launches != 0 {
		t.Errorf("launches = %d, want 0", launches)
	}
}

func TestDaemonEnvForLevelReplacesCaseInsensitiveDuplicates(t *testing.T) {
	parent := []string{
		"PATH=value",
		"SIDRAVIA_LOG_LEVEL=debug",
		"sidravia_log_level=trace",
	}
	original := append([]string(nil), parent...)
	got := daemonEnvForLevel(parent, "")
	if len(got) != 2 || got[0] != "PATH=value" || got[1] != "SIDRAVIA_LOG_LEVEL=info" {
		t.Errorf("environment = %v", got)
	}
	if strings.Join(parent, "\x00") != strings.Join(original, "\x00") {
		t.Errorf("parent environment mutated: got %v want %v", parent, original)
	}
}

func TestParseDaemonLogLevel(t *testing.T) {
	cases := []struct {
		args []string
		want string
		err  bool
	}{
		{nil, "", false},
		{[]string{}, "", false},
		{[]string{"--log-level", "info"}, "info", false},
		{[]string{"--log-level", "debug"}, "debug", false},
		{[]string{"--log-level", "trace"}, "trace", false},
		{[]string{"--log-level", "verbose"}, "", true},
		{[]string{"--log-level"}, "", true},
		{[]string{"extra"}, "", true},
		{[]string{"--log-level", "debug", "--log-level", "trace"}, "", true},
	}
	for _, c := range cases {
		got, err := parseDaemonLogLevel(c.args)
		if c.err {
			if err == nil {
				t.Errorf("parseDaemonLogLevel(%v) = nil, want error", c.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDaemonLogLevel(%v) = %v, want nil", c.args, err)
		}
		if got != c.want {
			t.Errorf("parseDaemonLogLevel(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestParseDaemonLogLevelDoesNotEchoInvalidValue(t *testing.T) {
	_, err := parseDaemonLogLevel([]string{"--log-level", "SECRET-LEVEL-MARKER"})
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
	if err != errCommandUsage {
		t.Errorf("err = %v, want errCommandUsage", err)
	}
	if strings.Contains(err.Error(), "SECRET-LEVEL-MARKER") {
		t.Errorf("invalid level value echoed: %v", err)
	}
}
