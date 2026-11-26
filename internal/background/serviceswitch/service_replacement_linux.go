//go:build linux

package serviceswitch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sidravia/internal/background/daemonctl"
	"time"

	"sidravia/internal/installation/userservice"
	"sidravia/internal/installation/userservice/registration"
	"sidravia/internal/process/linuxproc"
	"sidravia/internal/productlayout"
)

func replaceService(ctx context.Context, identity daemonctl.Identity, layout productlayout.Layout) (userservice.ServiceState, error) {
	var result userservice.ServiceState
	err := userservice.WithServiceSwitch(ctx, func(lease *userservice.SwitchLease) error {
		var operationErr error
		result, operationErr = replaceRegistration(ctx, lease, identity, layout)
		return operationErr
	})
	return result, err
}

func replaceRegistration(ctx context.Context, lease *userservice.SwitchLease, identity daemonctl.Identity, layout productlayout.Layout) (userservice.ServiceState, error) {
	var empty userservice.ServiceState
	if _, pending, err := userservice.PendingServiceSwitch(); err != nil || pending {
		return empty, errors.Join(userservice.ErrServiceRecovery, err)
	}
	old, found, err := registration.Lookup()
	if err != nil {
		return empty, err
	}
	before, err := lease.Service(ctx, "status", layout, nil)
	if err != nil {
		return before, err
	}
	ready := func(ctx context.Context, chosen productlayout.Layout) error {
		return daemonctl.WaitForService(ctx, identity, chosen)
	}
	if !found || old.ExecutableDirectory == layout.ExecutableDirectory {
		return lease.Service(ctx, "replace", layout, ready)
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return before, err
	}
	state := userservice.ServiceSwitch{
		ID: hex.EncodeToString(id), Layout: old, Mode: "replacement", Enabled: before.Enabled,
		TargetDirectory: layout.ExecutableDirectory, Expires: time.Now().Add(15 * time.Minute).UTC(),
	}
	var waitStopped func(context.Context) error
	if before.Running {
		info, err := daemonctl.ReadRuntimeInfo(old.RuntimeInfoPath)
		if err != nil {
			return before, errors.Join(daemonctl.ErrModeConflict, err)
		}
		proc, err := linuxproc.Observe(info.PID)
		if err != nil {
			return before, err
		}
		defer proc.Close()
		waitStopped = func(ctx context.Context) error {
			wait, cancel := context.WithTimeout(ctx, 17*time.Second)
			defer cancel()
			return proc.Wait(wait)
		}
		if proc.Executable != filepath.Join(old.ExecutableDirectory, "sidraviad") {
			return before, daemonctl.ErrModeConflict
		}
		oldIdentity := daemonctl.Identity{ProductVersion: info.ProductVersion, BuildID: info.BuildID}
		probe, err := daemonctl.ProbeRegisteredService(ctx, oldIdentity, old)
		if err != nil || probe.State != daemonctl.ProbeReachable || probe.Info.PID != info.PID || probe.Info.Token != info.Token {
			return before, errors.Join(daemonctl.ErrModeConflict, err)
		}
		alive, err := proc.Running()
		if err != nil || !alive {
			return before, errors.Join(daemonctl.ErrModeConflict, err)
		}
		state.RuntimePID, state.SourceVersion, state.SourceBuild = info.PID, info.ProductVersion, info.BuildID
		ready = func(ctx context.Context, chosen productlayout.Layout) error {
			if chosen.ExecutableDirectory == old.ExecutableDirectory {
				return daemonctl.WaitForService(ctx, oldIdentity, chosen)
			}
			return daemonctl.WaitForService(ctx, identity, chosen)
		}
	}
	if err := lease.Save(ctx, state); err != nil {
		return before, err
	}
	actual, err := lease.Replace(ctx, layout, waitStopped, ready)
	if err != nil {
		if actual.Directory != before.Directory || actual.Enabled != before.Enabled || actual.Running != before.Running {
			return actual, errors.Join(userservice.ErrServiceRecovery, err)
		}
		recover, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if before.Running {
			if readyErr := ready(recover, old); readyErr != nil {
				return actual, errors.Join(userservice.ErrServiceRecovery, err, readyErr)
			}
		}
		return clearReplacement(recover, lease, actual, err)
	}
	return clearReplacement(ctx, lease, actual, nil)
}

func clearReplacement(ctx context.Context, lease *userservice.SwitchLease, actual userservice.ServiceState, err error) (userservice.ServiceState, error) {
	if clearErr := lease.Clear(ctx); clearErr != nil {
		return actual, errors.Join(userservice.ErrServiceRecovery, err, clearErr)
	}
	actual.RecoveryPending = false
	actual.CanDiscardReplacement = false
	return actual, err
}

func recoverReplacement(ctx context.Context, lease *userservice.SwitchLease, state userservice.ServiceSwitch) (userservice.ServiceState, error) {
	var empty userservice.ServiceState
	if err := lease.Reload(ctx); err != nil {
		return empty, err
	}
	current, found, err := registration.Lookup()
	if err != nil || (found && current != state.Layout && current.ExecutableDirectory != state.TargetDirectory) {
		return empty, errors.Join(daemonctl.ErrModeConflict, err)
	}
	ready := func(ctx context.Context, layout productlayout.Layout) error {
		return daemonctl.WaitForService(ctx, daemonctl.Identity{ProductVersion: state.SourceVersion, BuildID: state.SourceBuild}, layout)
	}
	if found {
		if actual, err := lease.Service(ctx, "stop", current, nil); err != nil {
			return actual, err
		}
	}
	if !found || current != state.Layout {
		if actual, err := lease.Service(ctx, "replace", state.Layout, nil); err != nil {
			return actual, err
		}
	}
	enablement := "disable"
	if state.Enabled {
		enablement = "enable"
	}
	actual, err := lease.Service(ctx, enablement, state.Layout, nil)
	if err != nil {
		return actual, err
	}
	if state.RuntimePID != 0 {
		actual, err = lease.Service(ctx, "recover-start", state.Layout, ready)
		if err != nil {
			return actual, err
		}
	}
	err = lease.Clear(ctx)
	if err == nil {
		actual.RecoveryPending = false
		actual.CanDiscardReplacement = false
	}
	return actual, err
}

// Discard stops and removes only the recorded registration. It leaves both
// installations and their data in place and retires the journal last.
func discardReplacement(ctx context.Context, lease *userservice.SwitchLease, state userservice.ServiceSwitch) (userservice.ServiceState, error) {
	var empty userservice.ServiceState
	if err := lease.Reload(ctx); err != nil {
		return empty, err
	}
	current, found, err := registration.Lookup()
	if err != nil || (found && current != state.Layout && current.ExecutableDirectory != state.TargetDirectory) {
		return empty, errors.Join(daemonctl.ErrModeConflict, err)
	}
	layout := state.Layout
	if found {
		layout = current
	}
	actual, err := lease.Service(ctx, "unregister", layout, nil)
	if err != nil || actual.Registered || actual.Running || actual.Enabled {
		return actual, errors.Join(userservice.ErrServiceRecovery, err)
	}
	err = lease.Clear(ctx)
	if err == nil {
		actual.RecoveryPending = false
		actual.CanDiscardReplacement = false
	}
	return actual, err
}
