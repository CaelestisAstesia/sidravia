//go:build !windows

package jsonfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"sidravia/internal/daemon/persistence"
)

func newOtherTestStore(t *testing.T) *SecureStore {
	t.Helper()
	store, err := newSecureStoreWithOperations(SecureStoreOptions{}, newOtherSecureFileOperations())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOtherProtectionClassificationIsExact(t *testing.T) {
	for _, supported := range []error{syscall.ENOTSUP, syscall.EOPNOTSUPP} {
		if !errors.Is(classifyOtherProtection(supported), ProtectionUnsupported) {
			t.Fatalf("%v was not classified unsupported", supported)
		}
	}
	for _, ordinary := range []error{syscall.EACCES, syscall.EPERM, syscall.EIO, os.ErrInvalid} {
		if errors.Is(classifyOtherProtection(ordinary), ProtectionUnsupported) {
			t.Fatalf("%v was classified unsupported", ordinary)
		}
	}
}

func requireMode(t *testing.T, path string, wanted os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != wanted {
		t.Fatalf("mode(%q) = %#o, want %#o", filepath.Base(path), got, wanted)
	}
}

func TestOtherStoreCreatesOwnerOnlyDirectoryAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure", "state.json")
	if err := newOtherTestStore(t).Replace(context.Background(), path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	requireMode(t, filepath.Dir(path), 0700)
	requireMode(t, path, 0600)
}

func TestOtherStoreHardensExistingDirectoryAndFile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "secure")
	if err := os.MkdirAll(directory, 0777); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if err := newOtherTestStore(t).Replace(context.Background(), path, []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}
	requireMode(t, directory, 0700)
	requireMode(t, path, 0600)
}

func TestOtherStoreRenameReplacesWithinSameDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure", "state.json")
	store := newOtherTestStore(t)
	if err := store.Replace(context.Background(), path, []byte(`{"old":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(context.Background(), path, []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{\"new\":true}\n" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}

func TestOtherStoreRejectsSymlinkDestination(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "secure")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target.json")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "state.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := newOtherTestStore(t).Replace(context.Background(), link, []byte(`{"secret":true}`))
	requireFailureCode(t, err, persistence.FailurePermissionDenied)
	data, readErr := os.ReadFile(target)
	if readErr != nil || string(data) != "old" {
		t.Fatalf("symlink target = %q, %v", data, readErr)
	}
}

// The destination was absent at inspection but appears before the atomic commit.
// Exercise the real filesystem implementation rather than an in-memory commit.
type destinationRaceOperations struct {
	otherSecureFileOperations
}

func (operations *destinationRaceOperations) commit(tempPath, destinationPath string, destinationExists bool) (bool, error) {
	if err := os.WriteFile(destinationPath, []byte("existing recovery bytes"), 0600); err != nil {
		return false, err
	}
	return operations.otherSecureFileOperations.commit(tempPath, destinationPath, destinationExists)
}

func TestOtherStoreNewDestinationRacePreservesExistingBytes(t *testing.T) {
	for _, preserve := range []bool{false, true} {
		name := "replace"
		if preserve {
			name = "preserve"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "state.json")
			store, err := newSecureStoreWithOperations(SecureStoreOptions{}, &destinationRaceOperations{})
			if err != nil {
				t.Fatal(err)
			}
			if preserve {
				err = store.PreserveSensitive(context.Background(), path, []byte(`{"new":true}`), 1024, false)
			} else {
				err = store.Replace(context.Background(), path, []byte(`{"new":true}`))
			}
			requireFailureCode(t, err, persistence.FailureAtomicWrite)
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != "existing recovery bytes" {
				t.Fatalf("concurrent destination changed: %q, %v", data, readErr)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary file remained: %v, %v", entries, err)
			}
		})
	}
}
