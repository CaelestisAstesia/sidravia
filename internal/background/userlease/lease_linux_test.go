//go:build linux

package userlease_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"sidravia/internal/background/userlease"
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
	release, err := userlease.AcquireAt(dir, uid)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := userlease.AcquireAt(dir, uid); !errors.Is(err, userlease.ErrBusy) {
		if next != nil {
			next()
		}
		release()
		t.Fatalf("competing daemon: got %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, userlease.LeaseName)); !os.IsNotExist(err) {
		t.Fatalf("lease remained after exit: %v", err)
	}
	release, err = userlease.AcquireAt(dir, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
