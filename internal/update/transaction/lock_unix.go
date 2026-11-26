//go:build linux || darwin

package transaction

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lock(path string) (func(), error) { return lockMode(path, unix.LOCK_EX) }
func admissionLock(path string, execution bool) (func(), error) {
	if execution {
		return lockMode(path, unix.LOCK_EX)
	}
	return lockMode(path, unix.LOCK_SH)
}
func lockMode(path string, mode int) (func(), error) {
	file, err := lockFile(path, mode)
	if err != nil {
		return nil, err
	}
	return func() { _ = file.Close() }, nil
}
func lockFile(path string, mode int) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return nil, ErrInvalid
	}
	if err := unix.Fchmod(fd, 0600); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if err = unix.Flock(fd, mode|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.Join(ErrConflict, err)
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
