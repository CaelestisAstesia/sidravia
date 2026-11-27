//go:build linux

package startgate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxPackageGateBlocksStartupAndRecovers(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	gate := filepath.Join(root, "run", "rpm.lock")
	for _, dir := range []string{app, filepath.Dir(gate)} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(app, "sidravia.rpm"), []byte("sidravia-rpm-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(gate, unix.O_RDONLY|unix.O_CREAT, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := acquirePackageStart(app, gate, "rpm", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("active transaction allowed startup: %v", err)
	}
	unix.Flock(fd, unix.LOCK_UN)
	release, err := acquirePackageStart(app, gate, "rpm", "")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(app, "sidravia.rpm"), gate); err != nil {
		t.Fatal(err)
	}
	if _, err := acquirePackageStart(app, gate, "rpm", ""); !errors.Is(err, ErrBusy) {
		t.Fatal("symlink gate accepted")
	}
}

func TestDebianStartupRequiresPersistentGateAndUnblockedTransaction(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "lock")
	block := filepath.Join(root, "block")
	check := func(busy bool) {
		t.Helper()
		release, err := acquirePackageStart(root, lock, "deb", block)
		if busy {
			if !errors.Is(err, ErrBusy) {
				t.Fatalf("unguarded dpkg transition: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	check(true)
	if err := os.WriteFile(lock, nil, 0644); err != nil {
		t.Fatal(err)
	}
	check(true) // A missing package marker must not admit a partial DEB install.
	if err := os.WriteFile(filepath.Join(root, "sidravia.deb"), []byte("sidravia-deb-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.WriteFile(block, nil, 0644); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestPortableStartupRejectsLegacySeal(t *testing.T) {
	target := t.TempDir()
	release, err := AcquireStartIn(target)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.WriteFile(filepath.Join(target, "sidravia.update"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireStartIn(target); !errors.Is(err, ErrBusy) {
		t.Fatalf("sealed directory admitted: %v", err)
	}
}
