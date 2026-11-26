package daemonctl

import (
	"context"
	"errors"
	"os"
	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/productlayout"
	"testing"
)

func TestEnsureRejectsReachableModeConflictWithoutLaunch(t *testing.T) {
	owner := 42
	launches := 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return statusClient(t, "desktop", &owner), nil
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
		return statusClient(t, "desktop", &owner), nil
	})
	got, err := bootstrapDesktop(testIdentity(t), options, deps)
	if err != nil || got.Info.PID != 1 || got.Status.DesktopOwnerPID == nil || *got.Status.DesktopOwnerPID != owner {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestDesktopOptionsPreserveNamespace(t *testing.T) {
	ns, err := productlayout.NewNamespace("mock-test")
	if err != nil {
		t.Fatal(err)
	}
	deps := testDependencies(nil, nil)
	deps.namespace = func() (productlayout.Namespace, error) { return ns, nil }
	options, err := desktopOptionsWithNamespace(42, deps)
	if err != nil {
		t.Fatal(err)
	}
	if options.Mode != launchcontract.ModeDesktop || options.DesktopOwnerPID != 42 || options.Namespace != "mock-test" {
		t.Fatalf("options=%+v, want desktop owner 42 namespace mock-test", options)
	}
}

func TestBootstrapDesktopNamespaceFailureStopsBeforeProbeOrLaunch(t *testing.T) {
	reads, launches := 0, 0
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
		reads++
		return contract.RuntimeInfo{}, nil
	}, nil)
	deps.namespace = func() (productlayout.Namespace, error) {
		return productlayout.Namespace{}, productlayout.ErrInvalidNamespace
	}
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) {
		launches++
		return daemonLaunch{}, nil
	}
	if _, err := bootstrapDesktopWithDependencies(testIdentity(t), 42, deps); !errors.Is(err, productlayout.ErrInvalidNamespace) {
		t.Fatalf("err=%v, want invalid namespace", err)
	}
	if reads != 0 || launches != 0 {
		t.Fatalf("reads=%d launches=%d, want both zero", reads, launches)
	}
}

func TestDesktopSameOwnerReusesAndDifferentOwnerConflicts(t *testing.T) {
	owner := 42
	deps := testDependencies(func(string) (contract.RuntimeInfo, error) { return runtime(1), nil }, func(context.Context, contract.RuntimeInfo, Identity) (Client, error) {
		return statusClient(t, "desktop", &owner), nil
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
		return statusClient(t, "headless", nil), nil
	})
	deps.launch = func(launchcontract.Options, string) (daemonLaunch, error) { launches++; return daemonLaunch{}, nil }
	options, _ := launchcontract.Desktop(owner)
	if _, err := ensure(testIdentity(t), options, "", deps); !errors.Is(err, ErrModeConflict) || launches != 1 {
		t.Fatalf("err=%v launches=%d", err, launches)
	}
}
