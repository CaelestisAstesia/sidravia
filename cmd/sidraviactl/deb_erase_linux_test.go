package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/userservice"
)

func TestDebianEraseAuthorizationRequiresOnePacketFromParentForThisUID(t *testing.T) {
	const validGUI = "sidravia-deb-erase-v1\x00sidravia\x001000\x00"
	const validCore = "sidravia-deb-erase-v1\x00sidravia-core\x001000\x00"
	for _, tc := range []struct {
		name      string
		packets   []string
		kind      int
		parent    int
		peerUID   uint32
		noEOF     bool
		passFD    bool
		wantError bool
	}{
		{name: "gui", packets: []string{validGUI}},
		{name: "core", packets: []string{validCore}},
		{name: "wrong-peer-pid", packets: []string{validGUI}, parent: os.Getpid() + 1, wantError: true},
		{name: "wrong-peer-uid", packets: []string{validGUI}, peerUID: 1, wantError: true},
		{name: "stream", packets: []string{validGUI}, kind: unix.SOCK_STREAM, wantError: true},
		{name: "wrong-account", packets: []string{validGUI[:len(validGUI)-2] + "1\x00"}, wantError: true},
		{name: "wrong-package", packets: []string{"sidravia-deb-erase-v1\x00unrelated\x001000\x00"}, wantError: true},
		{name: "missing-frame-terminator", packets: []string{strings.TrimSuffix(validGUI, "\x00")}, wantError: true},
		{name: "oversized", packets: []string{validGUI + strings.Repeat("x", 64)}, wantError: true},
		{name: "extra-packet", packets: []string{validGUI, "extra"}, wantError: true},
		{name: "no-shutdown", packets: []string{validGUI}, noEOF: true, wantError: true},
		{name: "no-packet", wantError: true},
		{name: "received-descriptor", packets: []string{validGUI}, passFD: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := tc.kind
			if kind == 0 {
				kind = unix.SOCK_SEQPACKET
			}
			fds, err := unix.Socketpair(unix.AF_UNIX, kind|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fds[0])
			defer unix.Close(fds[1])
			var control []byte
			if tc.passFD {
				control = unix.UnixRights(fds[0])
			}
			for _, packet := range tc.packets {
				if err := unix.Sendmsg(fds[0], []byte(packet), control, nil, 0); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.noEOF {
				if err := unix.Shutdown(fds[0], unix.SHUT_WR); err != nil {
					t.Fatal(err)
				}
			}

			parent := tc.parent
			if parent == 0 {
				parent = os.Getpid()
			}
			err = readDebEraseAuthorization(fds[1], parent, 1000, uint32(os.Geteuid())+tc.peerUID)
			if tc.wantError && !errors.Is(err, userservice.ErrServiceConflict) || !tc.wantError && err != nil {
				t.Fatalf("want conflict=%v, got %v", tc.wantError, err)
			}
		})
	}
}

func TestDebianEraseRequiresProtectedStagingAndRemovedExecutables(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string, string) string
	}{
		{name: "removed"},
		{name: "alternate-executable", mutate: func(t *testing.T, root, state string) string {
			return filepath.Join(root, "sidraviactl")
		}},
		{name: "no-durable-block", mutate: func(t *testing.T, root, state string) string {
			if err := os.Remove(filepath.Join(state, "block")); err != nil {
				t.Fatal(err)
			}
			return ""
		}},
		{name: "incorrect-durable-block", mutate: func(t *testing.T, root, state string) string {
			writeDebEraseTestFile(t, filepath.Join(state, "block"), "fake\n", 0644)
			return ""
		}},
		{name: "unprotected-journal", mutate: func(t *testing.T, root, state string) string {
			if err := os.Chmod(filepath.Join(state, "transaction.json"), 0644); err != nil {
				t.Fatal(err)
			}
			return ""
		}},
		{name: "original-launcher-remains", mutate: func(t *testing.T, root, state string) string {
			writeDebEraseTestFile(t, filepath.Join(root, "sidravia_gui"), "still installed", 0755)
			return ""
		}},
		{name: "original-native-runner-remains", mutate: func(t *testing.T, root, state string) string {
			if err := os.Mkdir(filepath.Join(root, "gui-runtime"), 0755); err != nil {
				t.Fatal(err)
			}
			writeDebEraseTestFile(t, filepath.Join(root, "gui-runtime/sidravia_gui"), "still installed", 0755)
			return ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			root, state := filepath.Join(tmp, "payload"), filepath.Join(tmp, "maintenance")
			erase := filepath.Join(state, "erase")
			for _, path := range []string{root, state, erase} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			executable := filepath.Join(erase, "sidraviactl")
			writeDebEraseTestFile(t, executable, "staged CLI", 0755)
			info, err := os.Stat(executable)
			if err != nil {
				t.Fatal(err)
			}
			writeDebEraseTestFile(t, filepath.Join(state, "transaction.json"), "root-private journal", 0600)
			writeDebEraseTestFile(t, filepath.Join(state, "block"), "dpkg\n", 0644)
			writeDebEraseTestFile(t, filepath.Join(state, "lock"), "", 0644)
			if tc.mutate != nil {
				if other := tc.mutate(t, root, state); other != "" {
					executable = other
				}
			}
			err = verifyDebErase(root, state, executable, info, uint32(os.Geteuid()))
			if tc.mutate != nil && !errors.Is(err, userservice.ErrServiceConflict) || tc.mutate == nil && err != nil {
				t.Fatalf("want conflict=%v, got %v", tc.mutate != nil, err)
			}
		})
	}
}

func writeDebEraseTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
