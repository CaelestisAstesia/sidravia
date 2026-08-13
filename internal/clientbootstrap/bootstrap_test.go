package clientbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

type fakeClient struct {
	call  func(string, json.RawMessage) (contract.Response, error)
	close int
}

func (c *fakeClient) Call(_ context.Context, method string, payload json.RawMessage) (contract.Response, error) {
	return c.call(method, payload)
}
func (c *fakeClient) Close() error { c.close++; return nil }

func runtime(pid int) contract.RuntimeInfo {
	return contract.RuntimeInfo{Endpoint: "ws://127.0.0.1:1", Token: "token", BuildID: "build", PID: pid}
}

func statusResponse(t *testing.T) contract.Response {
	t.Helper()
	b, err := contract.MarshalStatusResult(contract.StatusResult{ProductVersion: "1", BuildID: "build", PID: 1, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewSuccessResponse("1", b)
}

func testDependencies(read func(string) (contract.RuntimeInfo, error), connect func(context.Context, contract.RuntimeInfo) (Client, error)) dependencies {
	return dependencies{
		runtimeInfoPath: func() (string, error) { return "runtime", nil },
		readRuntimeInfo: read,
		connect:         connect,
		launch:          func(string) (daemonLaunch, error) { return daemonLaunch{}, nil },
		callTimeout:     time.Second, totalWait: time.Second, pollInterval: time.Millisecond,
	}
}

func TestProbeClassifiesRuntimeState(t *testing.T) {
	cases := []struct {
		name string
		read func(string) (contract.RuntimeInfo, error)
		want ProbeState
	}{
		{"missing", func(string) (contract.RuntimeInfo, error) { return contract.RuntimeInfo{}, os.ErrNotExist }, ProbeStopped},
		{"malformed", func(string) (contract.RuntimeInfo, error) { return contract.RuntimeInfo{}, errors.New("bad") }, ProbeMalformed},
		{"unreachable", func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, ProbeUnreachable},
		{"reachable", func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, ProbeReachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connect := func(context.Context, contract.RuntimeInfo) (Client, error) { return nil, errors.New("unreachable") }
			if tc.want == ProbeReachable {
				connect = func(context.Context, contract.RuntimeInfo) (Client, error) {
					return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
				}
			}
			got, err := probe(testDependencies(tc.read, connect))
			if err != nil || got.State != tc.want {
				t.Fatalf("probe = %#v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestEnsureHotColdStaleAndReadiness(t *testing.T) {
	t.Run("hot", func(t *testing.T) {
		launches := 0
		deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo) (Client, error) {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		})
		deps.launch = func(string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
		outcome, err := ensure("", deps)
		if err != nil || outcome != AlreadyRunning || launches != 0 {
			t.Fatalf("outcome=%q err=%v launches=%d", outcome, err, launches)
		}
	})
	t.Run("cold", func(t *testing.T) {
		reachable, launches := false, 0
		deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
			if !reachable {
				return contract.RuntimeInfo{}, os.ErrNotExist
			}
			return runtime(1), nil
		}, func(context.Context, contract.RuntimeInfo) (Client, error) {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		})
		deps.launch = func(string) (daemonLaunch, error) { launches++; reachable = true; return daemonLaunch{}, nil }
		outcome, err := ensure("debug", deps)
		if err != nil || outcome != Started || launches != 1 {
			t.Fatalf("outcome=%q err=%v launches=%d", outcome, err, launches)
		}
	})
	t.Run("stale", func(t *testing.T) {
		launches := 0
		deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo) (Client, error) { return nil, errors.New("stale") })
		deps.launch = func(string) (daemonLaunch, error) { launches++; return daemonLaunch{}, errors.New("launch") }
		if _, err := ensure("", deps); err == nil || launches != 1 {
			t.Fatalf("err=%v launches=%d", err, launches)
		}
	})
}

func TestWaitForReadinessPreservesExitAndTimeoutContracts(t *testing.T) {
	cause := errors.New("exit")
	exited := make(chan error, 1)
	exited <- cause
	err := waitForReadiness(time.Second, time.Millisecond, exited, func() (bool, error) { return false, nil })
	if err == nil || !errors.Is(err, cause) {
		t.Fatalf("early exit = %v", err)
	}
	err = waitForReadiness(0, time.Millisecond, nil, func() (bool, error) { return false, nil })
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
}

func TestAcquireReturnsOneExactClient(t *testing.T) {
	operation := &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return contract.Response{}, nil }}
	connects := 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(7), nil }, func(context.Context, contract.RuntimeInfo) (Client, error) {
		connects++
		if connects < 3 {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		}
		return operation, nil
	})
	got, err := acquire(context.Background(), "", deps)
	if err != nil || got != operation || connects != 3 {
		t.Fatalf("got=%v err=%v connects=%d", got, err, connects)
	}
}
