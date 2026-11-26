package clientbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func TestEnsureRejectsAnsweredInvalidStatusWithoutLaunch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  contract.StatusResult
		options launchcontract.Options
		want    error
	}{
		{"product-version", contract.StatusResult{ProductVersion: "other", BuildID: "build", PID: 1, Mode: "headless"}, launchcontract.Headless(), ErrIncompatibleGeneration},
		{"build-id", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "other", PID: 1, Mode: "desktop", DesktopOwnerPID: intPtr(42)}, mustDesktop(t, 42), ErrIncompatibleGeneration},
		{"pid", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 2, Mode: "headless"}, launchcontract.Headless(), ErrModeConflict},
		{"mode", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "unknown"}, mustDesktop(t, 42), ErrModeConflict},
		{"headless-owner", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "headless", DesktopOwnerPID: intPtr(42)}, launchcontract.Headless(), ErrModeConflict},
		{"desktop-missing-owner", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "desktop"}, mustDesktop(t, 42), ErrModeConflict},
		{"desktop-zero-owner", contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Mode: "desktop", DesktopOwnerPID: intPtr(0)}, mustDesktop(t, 42), ErrModeConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launches := 0
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
				return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
					return statusResponseValue(t, tc.status), nil
				}}, nil
			})
			deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
			if _, err := ensure(testIdentity(t), tc.options, "", deps); !errors.Is(err, tc.want) || launches != 0 {
				t.Fatalf("err=%v launches=%d", err, launches)
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
		result, err := probe(testIdentity(t), deps)
		if err != nil || result.State != ProbeUnreachable || result.Status != nil {
			t.Fatalf("probe=%+v err=%v", result, err)
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

func TestEnsureColdLaunchPropagatesNamespace(t *testing.T) {
	ns, err := productlayout.NewNamespace("mock-test")
	if err != nil {
		t.Fatal(err)
	}
	reachable := false
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		if !reachable {
			return contract.RuntimeInfo{}, os.ErrNotExist
		}
		return runtime(1), nil
	}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
	})
	deps.namespace = func() (productlayout.Namespace, error) { return ns, nil }
	deps.launch = func(options launchcontract.Options, _ string) (daemonLaunch, error) {
		if options.Namespace != "mock-test" {
			t.Fatalf("launch namespace = %q, want mock-test", options.Namespace)
		}
		reachable = true
		return daemonLaunch{}, nil
	}
	options, err := headlessOptionsWithNamespace(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ensure(testIdentity(t), options, "", deps); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureFreezesNamespaceAcrossLaunchAndReadiness(t *testing.T) {
	ns, err := productlayout.NewNamespace("frozen-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := productlayout.NewNamespace("changed-b")
	if err != nil {
		t.Fatal(err)
	}
	namespaceCalls := 0
	pathNamespaces := []productlayout.Namespace{}
	launchedNamespace := ""
	reachable := false
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		if !reachable {
			return contract.RuntimeInfo{}, os.ErrNotExist
		}
		return runtime(1), nil
	}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
			return statusResponse(t), nil
		}}, nil
	})
	deps.namespace = func() (productlayout.Namespace, error) {
		namespaceCalls++
		if namespaceCalls == 1 {
			return ns, nil
		}
		return other, nil
	}
	deps.runtimeInfoPath = func(got productlayout.Namespace) (string, error) {
		pathNamespaces = append(pathNamespaces, got)
		return "runtime", nil
	}
	deps.launch = func(options launchcontract.Options, _ string) (daemonLaunch, error) {
		launchedNamespace = options.Namespace
		reachable = true
		return daemonLaunch{}, nil
	}
	if _, err := ensure(testIdentity(t), headlessOptions(ns), "", deps); err != nil {
		t.Fatal(err)
	}
	if namespaceCalls != 1 {
		t.Fatalf("namespace source calls = %d, want 1", namespaceCalls)
	}
	if launchedNamespace != ns.String() {
		t.Fatalf("launched namespace = %q, want %q", launchedNamespace, ns.String())
	}
	if len(pathNamespaces) == 0 {
		t.Fatal("runtime path was never resolved")
	}
	for _, got := range pathNamespaces {
		if got != ns {
			t.Fatalf("runtime path namespace = %q, want frozen %q", got.String(), ns.String())
		}
	}
}

func TestAcquireFreezesNamespaceForFinalConnection(t *testing.T) {
	ns, err := productlayout.NewNamespace("acquire-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := productlayout.NewNamespace("acquire-b")
	if err != nil {
		t.Fatal(err)
	}
	namespaceCalls, reads, connects := 0, 0, 0
	operation := &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return contract.Response{}, nil }}
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		reads++
		return runtime(1), nil
	}, func(_ context.Context, info contract.RuntimeInfo, _ Identity) (Client, error) {
		connects++
		if info.PID != 1 {
			t.Fatalf("connected PID = %d, want 1", info.PID)
		}
		if connects <= 2 {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) { return statusResponse(t), nil }}, nil
		}
		return operation, nil
	})
	deps.namespace = func() (productlayout.Namespace, error) {
		namespaceCalls++
		if namespaceCalls == 1 {
			return ns, nil
		}
		return other, nil
	}
	got, err := acquire(context.Background(), testIdentity(t), headlessOptions(ns), "", deps)
	if err != nil || got != operation {
		t.Fatalf("client = %v error = %v", got, err)
	}
	if namespaceCalls != 1 || reads != 2 || connects != 3 {
		t.Fatalf("namespace calls = %d, reads = %d, connects = %d", namespaceCalls, reads, connects)
	}
}

func TestContendedReadinessWaitsForMatchingPeer(t *testing.T) {
	for _, wantErr := range []error{nil, ErrModeConflict, ErrIncompatibleGeneration} {
		cause := errors.New("owned-child-exit")
		exited := make(chan error, 1)
		exited <- errors.Join(errDaemonSingletonContention, cause)
		close(exited)
		attempts := 0
		e := waitForReadiness(100*time.Millisecond, time.Millisecond, exited, func() (bool, error) {
			attempts++
			if attempts == 1 {
				return false, nil
			}
			return wantErr == nil, wantErr
		})
		if wantErr == nil && e != nil || wantErr != nil && !errors.Is(e, wantErr) || attempts < 2 {
			t.Fatalf("contention probes=%d error=%v", attempts, e)
		}
	}
}
func TestContendedReadinessTimeoutRetainsCause(t *testing.T) {
	cause := errors.New("owned-child-exit")
	exited := make(chan error, 1)
	exited <- errors.Join(errDaemonSingletonContention, cause)
	close(exited)
	e := waitForReadiness(10*time.Millisecond, time.Millisecond, exited, func() (bool, error) { return false, nil })
	if !errors.Is(e, context.DeadlineExceeded) || !errors.Is(e, cause) {
		t.Fatalf("contention timeout=%v", e)
	}
	if strings.Contains(e.Error(), "owned-child-exit") {
		t.Fatal("private exit cause leaked")
	}
}
func TestContendedReadinessEnsureDelayedPublication(t *testing.T) {
	for _, mode := range []string{"headless", "desktop", "incompatible"} {
		reads, launches := 0, 0
		deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
			reads++
			if reads <= 2 {
				return contract.RuntimeInfo{}, os.ErrNotExist
			}
			info := runtime(1)
			if mode == "incompatible" {
				info.BuildID = "other"
			}
			return info, nil
		}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
			return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
				if mode == "desktop" {
					owner := 42
					return statusResponseFor(t, "desktop", &owner), nil
				}
				return statusResponse(t), nil
			}}, nil
		})
		deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) {
			launches++
			c := make(chan error, 1)
			c <- errors.Join(errDaemonSingletonContention, errors.New("owned-child-exit"))
			return daemonLaunch{exited: c}, nil
		}
		deps.totalWait = 50 * time.Millisecond
		deps.pollInterval = time.Millisecond
		outcome, e := ensure(testIdentity(t), launchcontract.Headless(), "", deps)
		if launches != 1 {
			t.Fatal("contention spawned another child")
		}
		switch mode {
		case "headless":
			if e != nil || outcome != Started {
				t.Fatalf("matching delayed peer=%s %v", outcome, e)
			}
		case "desktop":
			if !errors.Is(e, ErrModeConflict) {
				t.Fatalf("mode/owner conflict=%v", e)
			}
		default:
			if !errors.Is(e, ErrIncompatibleGeneration) {
				t.Fatalf("build conflict=%v", e)
			}
		}
	}
}

// A restart's first replacement can lose to a shutting-down singleton after
// its IPC endpoint has gone. The reserved child result, not error text, is the
// only permission to retry; all attempts share the original deadline/budget.
func TestRestartReadinessRelaunch(t *testing.T) {
	for _, scenario := range []string{"released", "peer", "foreign", "ordinary", "contended"} {
		t.Run(scenario, func(t *testing.T) {
			exited := make(chan error, 1)
			if scenario == "ordinary" {
				exited <- errors.New("not singleton")
			} else {
				exited <- errDaemonSingletonContention
			}
			calls, probes := 0, 0
			err := waitForReadinessWithRelaunch(15*time.Millisecond, time.Millisecond, exited, func() (bool, error) {
				probes++
				if scenario == "foreign" {
					return false, ErrModeConflict
				}
				return scenario == "peer" || scenario == "released" && calls == 1, nil
			}, func() (daemonLaunch, error) {
				calls++
				next := make(chan error, 1)
				if scenario == "contended" {
					next <- errDaemonSingletonContention
				}
				return daemonLaunch{exited: next}, nil
			})
			switch scenario {
			case "released":
				if err != nil || calls != 1 {
					t.Fatalf("retry=%d err=%v", calls, err)
				}
			case "peer":
				if err != nil || calls != 0 {
					t.Fatalf("peer retried=%d err=%v", calls, err)
				}
			case "foreign":
				if !errors.Is(err, ErrModeConflict) || calls != 0 {
					t.Fatalf("foreign retried=%d err=%v", calls, err)
				}
			case "ordinary":
				if err == nil || calls != 0 {
					t.Fatalf("ordinary retried=%d err=%v", calls, err)
				}
			case "contended":
				if !errors.Is(err, context.DeadlineExceeded) || calls != 2 {
					t.Fatalf("unbounded retry=%d probes=%d err=%v", calls, probes, err)
				}
			}
		})
	}
}

func TestCanceledAcquisitionNeverStartsDaemon(t *testing.T) {
	for _, when := range []string{"before", "after-discovery", "readiness"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads, launches := 0, 0
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
				reads++
				if when == "after-discovery" || when == "readiness" && reads > 1 {
					cancel()
				}
				return contract.RuntimeInfo{}, os.ErrNotExist
			}, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
				t.Fatal("canceled acquisition reached transport")
				return nil, nil
			})
			deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) {
				launches++
				return daemonLaunch{}, nil
			}
			if when == "before" {
				cancel()
			}
			_, err := acquire(ctx, testIdentity(t), launchcontract.Headless(), "", deps)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation was lost: %v", err)
			}
			wantLaunches := 0
			if when == "readiness" {
				wantLaunches = 1
			}
			if launches != wantLaunches || when == "before" && reads != 0 {
				t.Fatalf("work continued after cancellation: reads=%d launches=%d", reads, launches)
			}
		})
	}
}

func TestAcquisitionCancellationInterruptsStatusProbe(t *testing.T) {
	for _, existingOnly := range []bool{false, true} {
		name := "acquire"
		if existingOnly {
			name = "existing-only"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			finished := make(chan error, 1)
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil },
				func(probeCtx context.Context, _ contract.RuntimeInfo, _ Identity) (Client, error) {
					close(entered)
					<-probeCtx.Done()
					return nil, probeCtx.Err()
				})
			// Longer than the assertion bound so the caller, not the probe's own
			// timeout, must release the pending transport operation.
			deps.callTimeout = 5 * time.Second
			identity := testIdentity(t)
			go func() {
				var err error
				if existingOnly {
					_, err = connectExistingHeadless(ctx, identity, deps)
				} else {
					_, err = acquire(ctx, identity, launchcontract.Headless(), "", deps)
				}
				finished <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("probe did not reach transport")
			}
			cancel()
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation was lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("probe ignored caller cancellation")
			}
		})
	}
}
