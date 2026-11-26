//go:build linux

package startgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/update/transaction/admission"
)

var ErrBusy = errors.New("installation maintenance is active or its gate is unavailable")

// AcquireStart returns a runtime admission lease. RPM uses its transaction
// gate; other Linux targets retain a portable shared lease until process exit.
func AcquireStart() (func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(exe)
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

func systemDirectory(directory string) bool {
	return directory == productlayout.RPMExecutableDirectory || directory == productlayout.DEBExecutableDirectory
}

func acquirePortableStart(directory string) (func(), error) {
	// Check for records even if the portable marker has been removed or changed:
	// a partial update must not turn into an unguarded installed launch.
	release, err := admission.Acquire(context.Background(), directory)
	if err != nil {
		return nil, errors.Join(ErrBusy, err)
	}
	return release, nil
}

// AcquireRun retains only portable admission for the process lifetime. RPM's
// transaction must be able to acquire its lock before requesting normal exit.
func AcquireRun() (func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	release, err := AcquireStart()
	if err != nil {
		return nil, err
	}
	if systemDirectory(filepath.Dir(exe)) {
		release()
		return func() {}, nil
	}
	return release, nil
}

// AcquireRunIdentity admits a cold recovery only for the recorded new build and
// coordinator ancestry. Completed operations use ordinary admission and shed
// stale recovery environment before any later child is launched.
func AcquireRunIdentity(version, build string) (func(), error) {
	releaseRun, err := AcquireRun()
	if err == nil {
		clearRecoveryEnvironment()
		return releaseRun, nil
	}
	exe, exeErr := os.Executable()
	if exeErr != nil || systemDirectory(filepath.Dir(exe)) {
		return nil, err
	}
	return admission.AcquireRecovery(context.Background(), filepath.Dir(exe), err, admission.RecoveryFromEnvironment(version, build))
}

// ForGUIExec is the portable GUI's only route to inherited admission. RPM
// keeps its short start gate so an RPM update can still stop the GUI later.
func ForGUIExec(directory, version, build string) (lease *os.File, recovery bool, err error) {
	if systemDirectory(directory) {
		release, err := AcquireStart()
		if err != nil {
			return nil, false, err
		}
		release()
		return nil, false, nil
	}
	return admission.AcquireForExec(context.Background(), directory, admission.RecoveryFromEnvironment(version, build))
}

func clearRecoveryEnvironment() {
	for _, name := range []string{launchcontract.EnvRecoveryOperation, launchcontract.EnvRecoveryToken, launchcontract.EnvRecoveryConfiguration} {
		os.Unsetenv(name)
	}
}
