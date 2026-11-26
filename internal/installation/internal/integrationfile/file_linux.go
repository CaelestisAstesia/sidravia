//go:build linux

package integrationfile

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrUnsafeScratch = errors.New("unsafe integration staging file")

// ClearScratch removes only a regular, uniquely linked staging file owned by the current user.
func ClearScratch(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 ||
		!info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return ErrUnsafeScratch
	}
	return os.Remove(path)
}

// Save commits a replacement through the owned staging path or removes the destination for nil contents.
// The caller holds the directory lease and verifies ownership of the destination.
func Save(path string, contents []byte) error {
	staging := path + ".sidravia-tmp"
	if err := ClearScratch(staging); err != nil {
		return err
	}
	if contents == nil {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	file, err := os.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(staging)
	_, writeErr := file.Write(contents)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Rename(staging, path)
}
