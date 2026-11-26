//go:build linux

package startgate

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestPortableGateRejectsUnverifiableStateEvenWithoutMarker(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	target := t.TempDir()
	release, err := acquirePortableStart(target)
	if err != nil {
		t.Fatal(err)
	}
	release()
	sum := sha256.Sum256([]byte(target))
	record := filepath.Join(cache, "Sidravia", "updates", "operations", hex.EncodeToString(sum[:]), "current.json")
	if err := os.WriteFile(record, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := acquirePortableStart(target); !errors.Is(err, ErrBusy) {
		t.Fatalf("unverifiable partial update allowed startup: %v", err)
	}
}

func TestGUIExecLeaseHoldsAdmissionAndStartsCloseOnExec(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	target := t.TempDir()
	lease, recovery, err := ForGUIExec(target, "1.0.0", "dev")
	if err != nil || recovery || lease == nil {
		t.Fatalf("ordinary GUI startup: lease=%v recovery=%v err=%v", lease, recovery, err)
	}
	defer lease.Close()
	flags, err := unix.FcntlInt(lease.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("launcher lease leaked before explicit inheritance: flags=%d err=%v", flags, err)
	}
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("test could not simulate exclusive installation: %v", err)
	}
	if other, _, err := ForGUIExec(target, "1.0.0", "dev"); err == nil {
		other.Close()
		t.Fatal("GUI inherited without a compatible startup lease")
	}
}
