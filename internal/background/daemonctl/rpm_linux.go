//go:build linux

package daemonctl

import (
	"context"
	"errors"
	"path/filepath"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/linuxproc"
)

// CheckPackageServiceIPC is the read-only RPM recovery check for an observed main
// process. The caller also checks that systemd still reports that invocation.
func CheckPackageServiceIPC(ctx context.Context, identity Identity, pid int, manager string) error {
	layout, found, err := serviceLayout(productlayout.Namespace{})
	owner, _ := productlayout.SystemPackage(layout)
	if err != nil || !found || owner == "" || owner != manager || layout.ExecutableDirectory != productlayout.PackageDirectory(manager) {
		return errors.Join(ErrModeConflict, err)
	}
	process, err := linuxproc.Observe(pid)
	if err != nil {
		return err
	}
	defer process.Close()
	if process.Executable != filepath.Join(layout.ExecutableDirectory, "sidraviad") {
		return ErrModeConflict
	}
	probe, err := ProbeRegisteredService(ctx, identity, layout)
	if err != nil || probe.State != ProbeReachable || probe.Info.PID != pid {
		return errors.Join(ErrConnectionUnavailable, err)
	}
	alive, err := process.Running()
	if err != nil || !alive {
		return errors.Join(ErrConnectionUnavailable, err)
	}
	return nil
}
