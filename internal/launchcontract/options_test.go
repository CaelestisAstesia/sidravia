package launchcontract

import (
	"errors"
	"slices"
	"testing"
)

func TestParseValidatesModesAndOwner(t *testing.T) {
	for _, tc := range []struct {
		mode, owner, namespace string
		want                   Options
		bad                    bool
	}{
		{"", "", "", Headless(), false},
		{"headless", "", "", Headless(), false},
		{"desktop", "42", "", Options{Mode: ModeDesktop, DesktopOwnerPID: 42}, false},
		{"headless", "42", "", Options{}, true},
		{"desktop", "", "", Options{}, true},
		{"desktop", "0", "", Options{}, true},
		{"desktop", "-1", "", Options{}, true},
		{"desktop", "bad", "", Options{}, true},
		{"other", "", "", Options{}, true},
		{"headless", "", "isolated-1", Options{Mode: ModeHeadless, Namespace: "isolated-1"}, false},
		{"headless", "", "bad/name", Options{}, true},
		{"desktop", "42", "isolated-1", Options{Mode: ModeDesktop, DesktopOwnerPID: 42, Namespace: "isolated-1"}, false},
	} {
		got, err := Parse(tc.mode, tc.owner, tc.namespace)
		if tc.bad {
			if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("Parse(%q,%q) err=%v", tc.mode, tc.owner, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("Parse(%q,%q)=%+v,%v want %+v", tc.mode, tc.owner, got, err, tc.want)
		}
	}
}

func TestChildEnvironmentIsExactAndDoesNotMutateParent(t *testing.T) {
	parent := []string{"PATH=x", "sidravia_daemon_mode=desktop", "SIDRAVIA_DESKTOP_OWNER_PID=99", "sidravia_log_level=trace"}
	before := append([]string(nil), parent...)
	opts, err := Desktop(42)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ChildEnvironment(parent, opts, "debug")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PATH=x", "SIDRAVIA_DAEMON_MODE=desktop", "SIDRAVIA_LOG_LEVEL=debug", "SIDRAVIA_DESKTOP_OWNER_PID=42"}
	if !slices.Equal(got, want) {
		t.Fatalf("env=%v want %v", got, want)
	}
	if !slices.Equal(parent, before) {
		t.Fatalf("parent mutated: %v", parent)
	}
}

func TestChildEnvironmentPropagatesNamespaceAndStripsInherited(t *testing.T) {
	parent := []string{"PATH=x", "sidravia_namespace=leaked", "SIDRAVIA_NAMESPACE=stale"}
	before := append([]string(nil), parent...)
	options, err := New(ModeHeadless, 0, "isolated-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ChildEnvironment(parent, options, "debug")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PATH=x", "SIDRAVIA_DAEMON_MODE=headless", "SIDRAVIA_LOG_LEVEL=debug", "SIDRAVIA_NAMESPACE=isolated-1"}
	if !slices.Equal(got, want) {
		t.Fatalf("env=%v want %v", got, want)
	}
	if !slices.Equal(parent, before) {
		t.Fatalf("parent mutated: %v", parent)
	}
}

func TestChildEnvironmentProductionDropsNamespaceKey(t *testing.T) {
	parent := []string{"SIDRAVIA_NAMESPACE=stale", "PATH=x"}
	got, err := ChildEnvironment(parent, Headless(), "info")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "SIDRAVIA_NAMESPACE=stale") {
		t.Fatalf("production child retained namespace key: %v", got)
	}
}
