package launchcontract

import (
	"errors"
	"slices"
	"testing"
)

func TestParseValidatesModesAndOwner(t *testing.T) {
	for _, tc := range []struct {
		mode, owner string
		want        Options
		bad         bool
	}{
		{"", "", Headless(), false},
		{"headless", "", Headless(), false},
		{"desktop", "42", Options{Mode: ModeDesktop, DesktopOwnerPID: 42}, false},
		{"headless", "42", Options{}, true},
		{"desktop", "", Options{}, true},
		{"desktop", "0", Options{}, true},
		{"desktop", "-1", Options{}, true},
		{"desktop", "bad", Options{}, true},
		{"other", "", Options{}, true},
	} {
		got, err := Parse(tc.mode, tc.owner)
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
