//go:build linux

package daemonctl

import (
	"context"
	"errors"
	"path/filepath"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/linuxproc"
)

// CheckRPMServiceIPC is the read-only RPM recovery check for an observed main
// process. The caller also checks that systemd still reports that invocation.
func CheckRPMServiceIPC(ctx context.Context, identity Identity, pid int) error {
	layout, found, err := serviceLayout(productlayout.Namespace{})
	if err != nil || !found || !productlayout.RPMManaged(layout) {
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
