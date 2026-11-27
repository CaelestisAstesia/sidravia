//go:build linux

package startgate

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
)

var ErrBusy = errors.New("installation maintenance is active or its gate is unavailable")

// AcquireStart checks the system package gate or rejects a directory left
// sealed by an interrupted update from an older portable release.
func AcquireStart() (func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return AcquireStartIn(filepath.Dir(exe))
}

// AcquireStartIn also serves the static GUI launcher before it loads GTK.
func AcquireStartIn(directory string) (func(), error) {
	switch directory {
	case productlayout.DEBExecutableDirectory:
		return acquirePackageStart(directory, "/var/lib/sidravia/deb-maintenance/lock", "deb", "/var/lib/sidravia/deb-maintenance/block")
	case productlayout.RPMExecutableDirectory:
		return acquirePackageStart(directory, "/run/sidravia/rpm.lock", "rpm", "")
	default:
		return acquirePortableStart(directory)
	}
}

func acquirePackageStart(directory, path, manager, block string) (func(), error) {
	marker, err := os.ReadFile(filepath.Join(directory, "sidravia."+manager))
	if (err != nil && (manager != "rpm" || !os.IsNotExist(err))) || (err == nil && string(marker) != "sidravia-"+manager+"-v1\n") {
		return nil, ErrBusy
	}
	var owner unix.Stat_t
	if err := unix.Stat(directory, &owner); err != nil {
		return nil, ErrBusy
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) && manager == "rpm" {
		return func() {}, nil
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
	return checkMaintenanceBlock(block, func() { unix.Close(fd) })
}

// dpkg calls the package scripts in separate processes. The durable block
// excludes ordinary starts between callbacks and after a host reboot.
func checkMaintenanceBlock(path string, release func()) (func(), error) {
	if path != "" {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			release()
			return nil, ErrBusy
		}
	}
	return release, nil
}

func acquirePortableStart(directory string) (func(), error) {
	if _, err := os.Lstat(filepath.Join(directory, "sidravia.update")); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrBusy
	}
	return func() {}, nil
}

func AcquireRun() (func(), error) {
	release, err := AcquireStart()
	if err != nil {
		return nil, err
	}
	release()
	return func() {}, nil
}

func AcquireRunIdentity(_, _ string) (func(), error) {
	release, err := AcquireRun()
	if err == nil {
		for _, name := range []string{launchcontract.EnvRecoveryOperation, launchcontract.EnvRecoveryToken, launchcontract.EnvRecoveryConfiguration} {
			os.Unsetenv(name)
		}
	}
	return release, err
}
