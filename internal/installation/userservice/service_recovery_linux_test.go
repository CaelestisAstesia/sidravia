//go:build linux

package userservice

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"sidravia/internal/installation/userservice/registration"
)

func TestSocketListening(t *testing.T) {
	runtime, err := os.MkdirTemp("", "sidravia-bus-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtime)
	path := filepath.Join(runtime, "bus")
	check := func(want bool) {
		t.Helper()
		if got, err := socketListening(context.Background(), path); err != nil || got != want {
			t.Fatalf("listening = %t, error = %v, want %t", got, err, want)
		}
	}
	check(false)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	t.Cleanup(func() { _ = listener.Close() })
	check(true)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := socketListening(context.Background(), path); !errors.Is(err, ErrServiceConflict) {
		t.Fatalf("foreign bus path: %v", err)
	}
}

func TestDiscardDamagedServiceSwitchProtectsForeignRecord(t *testing.T) {
	dir := t.TempDir()
	path := switchPath(filepath.Join(dir, registration.UnitName))
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "hardlink")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if err := discardDamagedServiceSwitch(path); !errors.Is(err, ErrServiceConflict) {
		t.Fatalf("hard-linked record: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{broken" {
		t.Fatalf("foreign record changed: %q, %v", data, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := discardDamagedServiceSwitch(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discarded record: %v", err)
	}
}

func TestRemoveRPMEnableLink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		remove bool
	}{
		{name: "owned", target: "../" + registration.UnitName, remove: true},
		{name: "foreign", target: "../other/" + registration.UnitName},
		{name: "regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			unit := filepath.Join(dir, registration.UnitName)
			wants := filepath.Join(dir, "default.target.wants")
			if err := os.Mkdir(wants, 0700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(wants, registration.UnitName)
			var err error
			if tc.target != "" {
				err = os.Symlink(tc.target, link)
			} else {
				err = os.WriteFile(link, []byte("foreign"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := removeRPMEnableLink(unit); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.remove:
				if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owned link still present: %v", err)
				}
			case tc.target != "":
				if target, err := os.Readlink(link); err != nil || target != tc.target {
					t.Fatalf("foreign link changed: %q, %v", target, err)
				}
			default:
				if data, err := os.ReadFile(link); err != nil || string(data) != "foreign" {
					t.Fatalf("foreign file changed: %q, %v", data, err)
				}
			}
		})
	}
}
