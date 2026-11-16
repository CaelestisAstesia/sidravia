//go:build linux

package installationgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"

	"golang.org/x/sys/unix"
)

var ErrBusy = errors.New("installation maintenance is active or its gate is unavailable")

// AcquireStart returns a runtime admission lease. RPM uses its transaction
// gate; other Linux targets retain a portable shared lease until process exit.
func AcquireStart() (func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if filepath.Dir(exe) != "/usr/lib64/sidravia" {
		return acquirePortableStart(filepath.Dir(exe))
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

func acquirePortableStart(directory string) (func(), error) {
	// Check for records even if the portable marker has been removed or changed:
	// a partial update must not turn into an unguarded installed launch.
	root, err := operations.DefaultRoot()
	if err != nil {
		return nil, errors.Join(ErrBusy, err)
	}
	store, err := operations.Open(root, directory, releases.PublisherTrust())
	if err != nil {
		return nil, errors.Join(ErrBusy, err)
	}
	release, err := store.AcquireStart(context.Background())
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
	if filepath.Dir(exe) == "/usr/lib64/sidravia" {
		release()
		return func() {}, nil
	}
	return release, nil
}

// AcquireRunIdentity admits a cold recovery only for the recorded new build and
// coordinator ancestry. Completed operations use ordinary admission and shed
// stale recovery environment before any later child is launched.
func AcquireRunIdentity(version, build string) (func(), error) {
	release, err := AcquireRun()
	if err == nil {
		clearRecoveryEnvironment()
		return release, nil
	}
	exe, exeErr := os.Executable()
	if exeErr != nil || filepath.Dir(exe) == "/usr/lib64/sidravia" {
		return nil, err
	}
	root, rootErr := operations.DefaultRoot()
	if rootErr != nil {
		return nil, errors.Join(err, rootErr)
	}
	store, openErr := operations.Open(root, filepath.Dir(exe), releases.PublisherTrust())
	if openErr != nil {
		return nil, errors.Join(err, openErr)
	}
	return store.AcquireRecoveryStart(context.Background(), os.Getenv(operations.EnvRecoveryOperation), os.Getenv(operations.EnvRecoveryToken), version, build, os.Getenv("SIDRAVIA_NAMESPACE"), os.Getenv(operations.EnvRecoveryConfiguration))
}
func clearRecoveryEnvironment() {
	for _, name := range []string{operations.EnvRecoveryOperation, operations.EnvRecoveryToken, operations.EnvRecoveryConfiguration} {
		os.Unsetenv(name)
	}
}
