//go:build linux

package installationgate

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
	if _, err := acquireLinuxStart(app, gate); !errors.Is(err, ErrBusy) {
		t.Fatalf("active transaction allowed startup: %v", err)
	}
	unix.Flock(fd, unix.LOCK_UN)
	release, err := acquireLinuxStart(app, gate)
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
	if _, err := acquireLinuxStart(app, gate); !errors.Is(err, ErrBusy) {
		t.Fatal("symlink gate accepted")
	}
}
