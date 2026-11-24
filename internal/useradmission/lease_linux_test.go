//go:build linux

package useradmission_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"sidravia/internal/useradmission"
)

func TestInstanceLeasePreventsConcurrentStartupAndCleansUp(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	uid := uint32(os.Geteuid())
	release, err := useradmission.AcquireAt(dir, uid)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := useradmission.AcquireAt(dir, uid); !errors.Is(err, useradmission.ErrBusy) {
		if next != nil {
			next()
		}
		release()
		t.Fatalf("competing daemon: got %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, useradmission.LeaseName)); !os.IsNotExist(err) {
		t.Fatalf("lease remained after exit: %v", err)
	}
	release, err = useradmission.AcquireAt(dir, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
