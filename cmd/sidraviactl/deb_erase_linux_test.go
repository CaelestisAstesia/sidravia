package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/userservice"
)

func TestDebianEraseRequiresCurrentRootTransactionAndRemovedExecutables(t *testing.T) {
	for _, tc := range []struct {
		name string
		test func(*testing.T, *debEraseFixture)
	}{
		{"live-remove", func(t *testing.T, f *debEraseFixture) {
			if _, err := f.journal.Seek(0, io.SeekEnd); err != nil {
				t.Fatal(err)
			}
			if err := f.verify(); err != nil {
				t.Fatal(err)
			}
			if err := f.verify(); err != nil {
				t.Fatal("a subsequent account must read the same journal:", err)
			}
		}},
		{"not-quiesced", func(t *testing.T, f *debEraseFixture) {
			writeDebEraseTestFile(t, f.journal.Name(), `{"package":"sidravia","action":"remove","phase":"blocking"}`, 0600)
		}},
		{"wrong-action", func(t *testing.T, f *debEraseFixture) {
			writeDebEraseTestFile(t, f.journal.Name(), `{"package":"sidravia","action":"upgrade","phase":"quiesced"}`, 0600)
		}},
		{"inactive-durable-block", func(t *testing.T, f *debEraseFixture) {
			if err := os.Remove(filepath.Join(f.state, "block")); err != nil {
				t.Fatal(err)
			}
		}},
		{"original-launcher-remains", func(t *testing.T, f *debEraseFixture) {
			writeDebEraseTestFile(t, filepath.Join(f.root, "sidravia_gui"), "still installed", 0755)
		}},
		{"original-native-runner-remains", func(t *testing.T, f *debEraseFixture) {
			if err := os.Mkdir(filepath.Join(f.root, "gui-runtime"), 0755); err != nil {
				t.Fatal(err)
			}
			writeDebEraseTestFile(t, filepath.Join(f.root, "gui-runtime/sidravia_gui"), "still installed", 0755)
		}},
		{"alternate-executable", func(t *testing.T, f *debEraseFixture) {
			f.executable = filepath.Join(f.root, "sidraviactl")
		}},
		{"journal-replaced", func(t *testing.T, f *debEraseFixture) {
			if err := os.Remove(f.journal.Name()); err != nil {
				t.Fatal(err)
			}
			writeDebEraseTestFile(t, f.journal.Name(), debEraseFixtureJournal, 0600)
		}},
		{"not-in-retry-set", func(t *testing.T, f *debEraseFixture) {
			writeDebEraseTestFile(t, f.journal.Name(), `{"package":"sidravia","action":"remove","phase":"quiesced","cleanupUIDs":[1001]}`, 0600)
		}},
		{"unlocked", func(t *testing.T, f *debEraseFixture) {
			if err := unix.Flock(int(f.lock.Fd()), unix.LOCK_UN); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong-lock-owner", func(t *testing.T, f *debEraseFixture) {
			f.pid++
		}},
		{"different-open-lock-description", func(t *testing.T, f *debEraseFixture) {
			f.lock = openDebEraseTestFile(t, f.lock.Name())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDebEraseFixture(t)
			tc.test(t, f)
			if tc.name != "live-remove" {
				if err := f.verify(); !errors.Is(err, userservice.ErrServiceConflict) {
					t.Fatalf("expected admission conflict, got %v", err)
				}
			}
		})
	}
}

const debEraseFixtureJournal = `{"package":"sidravia","action":"remove","phase":"quiesced","services":{}}`

type debEraseFixture struct {
	root, state, executable string
	exe                     os.FileInfo
	journal, lock           *os.File
	pid                     int
}

func (f *debEraseFixture) verify() error {
	return verifyDebErase(f.root, f.state, f.executable, f.exe, f.journal, f.lock, f.pid, 1000, uint32(os.Geteuid()))
}

func newDebEraseFixture(t *testing.T) *debEraseFixture {
	t.Helper()
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
	exe, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(state, "transaction.json")
	writeDebEraseTestFile(t, journalPath, debEraseFixtureJournal, 0600)
	writeDebEraseTestFile(t, filepath.Join(state, "block"), "dpkg\n", 0644)
	lockPath := filepath.Join(state, "lock")
	writeDebEraseTestFile(t, lockPath, "", 0644)
	lock := openDebEraseTestFile(t, lockPath)
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return &debEraseFixture{root, state, executable, exe, openDebEraseTestFile(t, journalPath), lock, os.Getpid()}
}

func writeDebEraseTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	// A restrictive umask should not change the fixture's intended modes.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func openDebEraseTestFile(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
