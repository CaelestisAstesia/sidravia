package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
)

func TestProbeRejectsInvalidAuthoritativeStatus(t *testing.T) {
	identity := testIdentity(t)
	for _, tc := range []struct {
		name   string
		status contract.StatusResult
	}{
		{"pid", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 2, Mode: "headless"}},
		{"mode", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "unknown"}},
		{"headless-owner", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "headless", DesktopOwnerPID: intPtr(42)}},
		{"desktop-missing-owner", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "desktop"}},
		{"desktop-zero-owner", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "desktop", DesktopOwnerPID: intPtr(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
				return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
					return statusResponseValue(t, tc.status), nil
				}}, nil
			})
			result, err := probe(identity, deps)
			if err != nil || result.State != ProbeUnreachable || result.Status == nil || result.Status.PID != tc.status.PID || result.Status.Mode != tc.status.Mode {
				t.Fatalf("probe=%+v err=%v", result, err)
			}
		})
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

func TestConnectExistingHeadlessStoppedNeverLaunches(t *testing.T) {
	launches := 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return contract.RuntimeInfo{}, os.ErrNotExist }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		t.Fatal("connect called")
		return nil, nil
	})
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
	if _, err := connectExistingHeadless(context.Background(), testIdentity(t), deps); !errors.Is(err, ErrDaemonStopped) || launches != 0 {
		t.Fatalf("err=%v launches=%d", err, launches)
	}
}

func TestConnectExistingHeadlessUsesExactProbedGeneration(t *testing.T) {
	reads, connects := 0, 0
	operation := &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return contract.Response{}, nil }}
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		reads++
		return runtime(1), nil
	}, func(_ context.Context, info contract.RuntimeInfo, _ Identity) (Client, error) {
		connects++
		if info.PID != 1 {
			t.Fatalf("connected PID=%d", info.PID)
		}
		if connects == 1 {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		}
		return operation, nil
	})
	got, err := connectExistingHeadless(context.Background(), testIdentity(t), deps)
	if err != nil || got != operation || reads != 1 || connects != 2 {
		t.Fatalf("got=%v err=%v reads=%d connects=%d", got, err, reads, connects)
	}
}

func TestConnectExistingHeadlessRejectsDesktopAndUnsafeStates(t *testing.T) {
	owner := 42
	desktop := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return statusClient(t, "desktop", &owner), nil
	})
	if _, err := connectExistingHeadless(context.Background(), testIdentity(t), desktop); !errors.Is(err, ErrModeConflict) {
		t.Fatalf("desktop err=%v", err)
	}
	malformed := testDependencies(func(string) (contract.RuntimeInfo, error) { return contract.RuntimeInfo{}, errors.New("private path") }, nil)
	if _, err := connectExistingHeadless(context.Background(), testIdentity(t), malformed); !errors.Is(err, ErrConnectionUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatalf("malformed err=%v", err)
	}
}

func TestProbeUsesNamespaceForRuntimeInfoPath(t *testing.T) {
	got, err := productlayout.NewNamespace("injected")
	if err != nil {
		t.Fatal(err)
	}
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		return contract.RuntimeInfo{}, os.ErrNotExist
	}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) { return nil, nil })
	deps.runtimeInfoPath = func(ns productlayout.Namespace) (string, error) {
		got = ns
		return "runtime", nil
	}
	deps.namespace = func() (productlayout.Namespace, error) { return productlayout.NewNamespace("mock-test") }
	if _, err := probe(testIdentity(t), deps); err != nil {
		t.Fatal(err)
	}
	if got.String() != "mock-test" {
		t.Fatalf("runtimeInfoPath namespace = %q, want mock-test", got)
	}
}

func TestCanceledExistingConnectionDoesNotProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reads := 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		reads++
		return contract.RuntimeInfo{}, os.ErrNotExist
	}, nil)
	_, err := connectExistingHeadless(ctx, testIdentity(t), deps)
	if !errors.Is(err, context.Canceled) || reads != 0 {
		t.Fatalf("canceled read-only connection: err=%v reads=%d", err, reads)
	}
}

func TestExistingConnectionPreservesCancellationAfterSuccessfulProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	connections := 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil },
		func(connectCtx context.Context, _ contract.RuntimeInfo, _ Identity) (Client, error) {
			connections++
			if connections == 1 {
				return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
					return statusResponse(t), nil
				}}, nil
			}
			close(entered)
			<-connectCtx.Done()
			return nil, connectCtx.Err()
		})
	deps.callTimeout = 5 * time.Second
	identity := testIdentity(t)
	go func() {
		_, err := connectExistingHeadless(ctx, identity, deps)
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("final connection was not attempted")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("final connection lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("final connection ignored cancellation")
	}
}
