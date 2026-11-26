//go:build linux

// Package userlease arbitrates production daemon launches and registration.
package userlease

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"sidravia/internal/productlayout"
)

const LeaseName = ".sidravia-daemon.lock"

var ErrBusy = errors.New("user daemon lease already held")

func Acquire() (func() error, error) {
	uid := os.Geteuid()
	home, err := productlayout.AccountHome(uid)
	if err != nil {
		return nil, err
	}
	dir, err := unix.Open(home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	release, err := AcquireAt(dir, uint32(uid))
	if err != nil {
		unix.Close(dir)
		return nil, err
	}
	return func() error { return errors.Join(release(), unix.Close(dir)) }, nil
}

func AcquireAt(dir int, uid uint32) (func() error, error) {
	var root unix.Stat_t
	if err := unix.Fstat(dir, &root); err != nil {
		return nil, err
	}
	if root.Uid != uid || root.Mode&0022 != 0 {
		return nil, errors.New("invalid account home ownership or permissions")
	}
	fd, err := unix.Openat(dir, LeaseName,
		unix.O_CREAT|unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	locked := false
	defer func() {
		if !locked {
			unix.Close(fd)
		}
	}()
	var actual unix.Stat_t
	if err := unix.Fstat(fd, &actual); err != nil {
		return nil, err
	}
	if actual.Uid != uid || actual.Mode != unix.S_IFREG|0600 || actual.Size != 0 || actual.Nlink != 1 {
		return nil, errors.New("invalid account instance lease")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	// An opener may still hold the old inode after the previous owner unlinks
	// it. Compare with the current directory entry after locking to reject it.
	matches := func() error {
		var current unix.Stat_t
		if err := unix.Fstatat(dir, LeaseName, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if current.Dev != actual.Dev || current.Ino != actual.Ino {
			return ErrBusy
		}
		return nil
	}
	if err := matches(); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, ErrBusy
		}
		return nil, err
	}
	locked = true
	return func() error {
		if err := matches(); err != nil {
			return errors.Join(err, unix.Close(fd))
		}
		return errors.Join(unix.Unlinkat(dir, LeaseName, 0), unix.Close(fd))
	}, nil
}
