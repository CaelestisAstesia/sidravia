//go:build linux

package installationgate

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var ErrBusy = errors.New("RPM maintenance is active or its gate is unavailable")

// AcquireStart shares a short startup lock with the RPM transaction. Once this
// check releases, our executable is visible to the transaction's pidfd scan.
// Portable/other installations never consult the system package gate.
func AcquireStart() (func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if filepath.Dir(exe) != "/usr/lib64/sidravia" {
		return func() {}, nil
	}
	return acquireLinuxStart(filepath.Dir(exe), "/run/sidravia/rpm.lock")
}

func acquireLinuxStart(directory, path string) (func(), error) {
	marker, err := os.ReadFile(filepath.Join(directory, "sidravia.rpm"))
	if (err != nil && !os.IsNotExist(err)) || (err == nil && string(marker) != "sidravia-rpm-v1\n") {
		return nil, ErrBusy
	}
	var owner unix.Stat_t
	if err := unix.Stat(directory, &owner); err != nil {
		return nil, ErrBusy
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return func() {}, nil // Before the first coordinating RPM transaction.
	}
	if err != nil {
		return nil, ErrBusy
	}
	fail := func() (func(), error) { unix.Close(fd); return nil, ErrBusy }
	var info, parent unix.Stat_t
	if unix.Fstat(fd, &info) != nil || unix.Lstat(filepath.Dir(path), &parent) != nil ||
		info.Mode&unix.S_IFMT != unix.S_IFREG || info.Size != 0 || info.Uid != owner.Uid || info.Mode&0022 != 0 ||
		parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Uid != owner.Uid || parent.Mode&0022 != 0 {
		return fail()
	}
	if unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB) != nil {
		return fail()
	}
	return func() { unix.Close(fd) }, nil
}
