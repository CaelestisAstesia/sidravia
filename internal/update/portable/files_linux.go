//go:build linux

package portable

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func singleFile(info os.FileInfo) error {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Nlink != 1 {
		return ErrChanged
	}
	return nil
}
func matchesMode(info os.FileInfo, mode uint32) bool {
	return info.Mode().Perm() == os.FileMode(mode) && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
}
func applicationPlatform(i Inventory) error {
	if i.manifest.OS != "linux" || i.manifest.Arch != runtime.GOARCH || i.manifest.PayloadKind != "core" {
		return ErrUnsupported
	}
	return nil
}
func syncDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	return errorsJoinClose(f.Sync(), f)
}
func availableSpace(root *os.Root, required int64) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err != nil {
		return err
	}
	if st.Bavail < uint64((required+int64(st.Bsize)-1)/int64(st.Bsize)) {
		return syscall.ENOSPC
	}
	return nil
}

// Supported core processes retain runtime admission leases. Execution admission
// proves those leases are gone, across namespaces. This supplemental scan also
// refuses visible nonparticipating target executables; inaccessible unrelated
// /proc entries are not evidence of a target process. No process is killed.
// Versions without lifetime admission require the manual baseline upgrade.
func ensureQuiescent(target string) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		info, err := os.Stat(filepath.Join("/proc", entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return ErrChanged
		}
		if stat.Uid != uint32(os.Geteuid()) {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrPermission) {
			continue
		}
		if err != nil {
			return err
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		relative, err := filepath.Rel(target, exe)
		if err != nil {
			return err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		// Linux can replace the running updater inode; its lifetime is finite. All
		// other executables under this installation must have exited normally.
		if pid == os.Getpid() && filepath.Base(exe) == "sidraviaupdater" {
			continue
		}
		return ErrChanged
	}
	return nil
}

func sameDevice(a, b os.FileInfo) bool {
	x, ok := a.Sys().(*syscall.Stat_t)
	y, other := b.Sys().(*syscall.Stat_t)
	return ok && other && x.Dev == y.Dev
}

// New paths are atomically no-replace: a file appearing after preflight remains
// user-owned. Open directory handles keep the rename beneath the parent root.
func publish(root *os.Root, old, new string, replace bool) error {
	source, err := root.OpenRoot(filepath.Dir(old))
	if err != nil {
		return err
	}
	defer source.Close()
	dest, err := root.OpenRoot(filepath.Dir(new))
	if err != nil {
		return err
	}
	defer dest.Close()
	sf, err := source.Open(".")
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := dest.Open(".")
	if err != nil {
		return err
	}
	defer df.Close()
	flags := uint(0)
	if !replace {
		flags = unix.RENAME_NOREPLACE
	}
	return unix.Renameat2(int(sf.Fd()), filepath.Base(old), int(df.Fd()), filepath.Base(new), flags)
}
