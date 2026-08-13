package clientbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
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
	return contract.RuntimeInfo{Endpoint: "ws://127.0.0.1:1", Token: "token", ProductVersion: "1.0.0", BuildID: "build", PID: pid}
}

func testIdentity(t *testing.T) Identity {
	t.Helper()
	identity, err := NewIdentity("1.0.0", "build")
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func statusResponse(t *testing.T) contract.Response {
	return statusResponseFor(t, "headless", nil)
}

func statusResponseFor(t *testing.T, mode string, owner *int) contract.Response {
	t.Helper()
	return statusResponseValue(t, contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Status: "running", Mode: mode, DesktopOwnerPID: owner})
}

func statusResponseValue(t *testing.T, value contract.StatusResult) contract.Response {
	t.Helper()
	b, err := contract.MarshalStatusResult(value)
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewSuccessResponse("1", b)
}

func TestEnsureRejectsReachableModeConflictWithoutLaunch(t *testing.T) {
	owner := 42
	launches := 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
			return statusResponseFor(t, "desktop", &owner), nil
		}}, nil
	})
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
	if _, err := ensure(testIdentity(t), launchcontract.Headless(), "", deps); !errors.Is(err, ErrModeConflict) || launches != 0 {
		t.Fatalf("err=%v launches=%d", err, launches)
	}
}

func TestBootstrapDesktopReturnsMatchingAuthoritativeResult(t *testing.T) {
	owner := 42
	options, err := launchcontract.Desktop(owner)
	if err != nil {
		t.Fatal(err)
	}
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
			return statusResponseFor(t, "desktop", &owner), nil
		}}, nil
	})
	got, err := bootstrapDesktop(testIdentity(t), options, deps)
	if err != nil || got.Info.PID != 1 || got.Status.DesktopOwnerPID == nil || *got.Status.DesktopOwnerPID != owner {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestDesktopSameOwnerReusesAndDifferentOwnerConflicts(t *testing.T) {
	owner := 42
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
			return statusResponseFor(t, "desktop", &owner), nil
		}}, nil
	})
	launches := 0
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
	matching, _ := launchcontract.Desktop(owner)
	if outcome, err := ensure(testIdentity(t), matching, "", deps); err != nil || outcome != AlreadyRunning || launches != 0 {
		t.Fatalf("same owner outcome=%q err=%v launches=%d", outcome, err, launches)
	}
	different, _ := launchcontract.Desktop(99)
	if _, err := ensure(testIdentity(t), different, "", deps); !errors.Is(err, ErrModeConflict) || launches != 0 {
		t.Fatalf("different owner err=%v launches=%d", err, launches)
	}
}

func TestDesktopReadinessConflictStopsWithoutFurtherLaunch(t *testing.T) {
	owner := 42
	reads, launches := 0, 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		reads++
		if reads == 1 {
			return contract.RuntimeInfo{}, os.ErrNotExist
		}
		return runtime(1), nil
	}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
			return statusResponseFor(t, "headless", nil), nil
		}}, nil
	})
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
	options, _ := launchcontract.Desktop(owner)
	if _, err := ensure(testIdentity(t), options, "", deps); !errors.Is(err, ErrModeConflict) || launches != 1 {
		t.Fatalf("err=%v launches=%d", err, launches)
	}
}

func TestProbeRejectsInvalidAuthoritativeStatus(t *testing.T) {
	identity := testIdentity(t)
	for _, status := range []contract.StatusResult{
		{ProductVersion: "1.0.0", BuildID: "build", PID: 2, Mode: "headless"},
		{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "unknown"},
		{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "headless", DesktopOwnerPID: intPtr(42)},
		{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "desktop"},
		{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "desktop", DesktopOwnerPID: intPtr(0)},
	} {
		t.Run(status.Mode, func(t *testing.T) {
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
				return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponseValue(t, status), nil }}, nil
			})
			result, err := probe(identity, deps)
			if err != nil || result.State != ProbeUnreachable {
				t.Fatalf("probe=%+v err=%v", result, err)
			}
		})
	}
}

func intPtr(value int) *int { return &value }

func testDependencies(read func(string) (contract.RuntimeInfo, error), connect func(context.Context, contract.RuntimeInfo, Identity) (Client, error)) dependencies {
	return dependencies{
		runtimeInfoPath: func() (string, error) { return "runtime", nil },
		readRuntimeInfo: read,
		connect:         connect,
		launch:          func(launchcontract.Options, string) (daemonLaunch, error) { return daemonLaunch{}, nil },
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
			connect := func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
				return nil, errors.New("unreachable")
			}
			if tc.want == ProbeReachable {
				connect = func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
					return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
				}
			}
			got, err := probe(testIdentity(t), testDependencies(tc.read, connect))
			if err != nil || got.State != tc.want {
				t.Fatalf("probe = %#v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestEnsureHotColdStaleAndReadiness(t *testing.T) {
	t.Run("hot", func(t *testing.T) {
		launches := 0
		deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		})
		deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
		outcome, err := ensure(testIdentity(t), launchcontract.Headless(), "", deps)
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
		}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		})
		deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) {
			launches++
			reachable = true
			return daemonLaunch{}, nil
		}
		outcome, err := ensure(testIdentity(t), launchcontract.Headless(), "debug", deps)
		if err != nil || outcome != Started || launches != 1 {
			t.Fatalf("outcome=%q err=%v launches=%d", outcome, err, launches)
		}
	})
	t.Run("stale", func(t *testing.T) {
		launches := 0
		deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) { return nil, errors.New("stale") })
		deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) {
			launches++
			return daemonLaunch{}, errors.New("launch")
		}
		if _, err := ensure(testIdentity(t), launchcontract.Headless(), "", deps); err == nil || launches != 1 {
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
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		connects++
		if connects < 3 {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		}
		return operation, nil
	})
	got, err := acquire(context.Background(), testIdentity(t), launchcontract.Headless(), "", deps)
	if err != nil || got != operation || connects != 3 {
		t.Fatalf("got=%v err=%v connects=%d", got, err, connects)
	}
}

func TestIdentityRejectsEmptyValues(t *testing.T) {
	for _, tc := range []Identity{{}, {ProductVersion: "1.0.0"}, {BuildID: "build"}} {
		if err := tc.validate(); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("validate(%+v) = %v", tc, err)
		}
	}
}

func TestIncompatibleGenerationDoesNotConnectOrLaunch(t *testing.T) {
	info := runtime(7)
	info.BuildID = "other"
	connects, launches := 0, 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return info, nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		connects++
		return nil, errors.New("must not connect")
	})
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
	result, err := probe(testIdentity(t), deps)
	if err != nil || result.State != ProbeIncompatible || connects != 0 {
		t.Fatalf("probe=%+v err=%v connects=%d", result, err, connects)
	}
	if _, err := ensure(testIdentity(t), launchcontract.Headless(), "", deps); !errors.Is(err, ErrIncompatibleGeneration) || connects != 0 || launches != 0 {
		t.Fatalf("ensure err=%v connects=%d launches=%d", err, connects, launches)
	}
}

func TestConnectPassesClientIdentityToTransport(t *testing.T) {
	identity := testIdentity(t)
	info := runtime(7)
	got, err := connectWithDependencies(context.Background(), identity, info, dependencies{connect: func(_ context.Context, gotInfo contract.RuntimeInfo, gotIdentity Identity) (Client, error) {
		if gotInfo != info || gotIdentity != identity {
			t.Fatalf("info=%+v identity=%+v", gotInfo, gotIdentity)
		}
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return contract.Response{}, nil }}, nil
	}})
	if err != nil || got == nil {
		t.Fatalf("Connect err=%v client=%v", err, got)
	}
}
