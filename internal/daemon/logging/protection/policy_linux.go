//go:build linux

package protection

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type policy struct{ ownerUID uint32 }

// New prepares the log protection policy for the current process owner.
func New() (Policy, error) {
	return policy{uint32(os.Geteuid())}, nil
}
func (p policy) Directory(path string) error { return p.apply(path, true) }
func (p policy) File(path string) error      { return p.apply(path, false) }

// Open without following the final link or blocking on a FIFO; validate the
// owned object before changing permissions. Ancestors must not redirect it.
func (p policy) apply(path string, directory bool) (result error) {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	for parent := absolute; ; parent = filepath.Dir(parent) {
		info, e := os.Lstat(parent)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("log path contains a symbolic link")
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	flags := syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	wantType, wantMode := uint32(syscall.S_IFREG), uint32(0600)
	if directory {
		flags |= syscall.O_DIRECTORY
		wantType, wantMode = syscall.S_IFDIR, 0700
	}
	fd, e := syscall.Open(absolute, flags, 0)
	if e != nil {
		return e
	}
	defer func() { result = errors.Join(result, syscall.Close(fd)) }()
	var info syscall.Stat_t
	if e := syscall.Fstat(fd, &info); e != nil {
		return e
	}
	if info.Mode&syscall.S_IFMT != wantType || info.Uid != p.ownerUID || (!directory && info.Nlink != 1) {
		return errors.New("log object is not a single owned regular object")
	}
	if e := syscall.Fchmod(fd, wantMode); e != nil {
		return e
	}
	if e := syscall.Fstat(fd, &info); e != nil {
		return e
	}
	if info.Mode&07777 != wantMode || info.Uid != p.ownerUID {
		return errors.New("log permission verification failed")
	}
	return nil
}
