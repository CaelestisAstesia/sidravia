//go:build linux

package clientbootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"sidravia/internal/installationmanager"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/linuxplatform"
)

func replaceService(ctx context.Context, identity Identity, layout productlayout.Layout) (installationmanager.ServiceState, error) {
	var empty installationmanager.ServiceState
	ctx, unlock, err := installationmanager.BeginServiceSwitch(ctx)
	if err != nil {
		return empty, err
	}
	defer unlock()
	if _, pending, err := installationmanager.PendingServiceSwitch(); err != nil || pending {
		return empty, errors.Join(installationmanager.ErrServiceRecovery, err)
	}
	old, found, err := installationmanager.RegisteredService()
	if err != nil {
		return empty, err
	}
	before, err := installationmanager.Service(ctx, "status", layout, nil)
	if err != nil {
		return before, err
	}
	ready := func(ctx context.Context, chosen productlayout.Layout) error {
		return WaitForService(ctx, identity, chosen)
	}
	if !found || old.ExecutableDirectory == layout.ExecutableDirectory {
		return installationmanager.Service(ctx, "replace", layout, ready)
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return before, err
	}
	state := installationmanager.ServiceSwitch{
		ID: hex.EncodeToString(id), Layout: old, Mode: "replacement", Enabled: before.Enabled,
		TargetDirectory: layout.ExecutableDirectory, Expires: time.Now().Add(15 * time.Minute).UTC(),
	}
	if before.Running {
		info, err := defaultDependencies().readRuntimeInfo(old.RuntimeInfoPath)
		if err != nil {
			return before, errors.Join(ErrModeConflict, err)
		}
		proc, err := linuxplatform.Observe(info.PID)
		if err != nil {
			return before, err
		}
		defer proc.Close()
		if proc.Executable != filepath.Join(old.ExecutableDirectory, "sidraviad") {
			return before, ErrModeConflict
		}
		oldIdentity := Identity{ProductVersion: info.ProductVersion, BuildID: info.BuildID}
		probe, err := serviceProbe(ctx, oldIdentity, old)
		if err != nil || probe.State != ProbeReachable || probe.Info.PID != info.PID {
			return before, errors.Join(ErrModeConflict, err)
		}
		state.RuntimePID, state.SourceVersion, state.SourceBuild = info.PID, info.ProductVersion, info.BuildID
		ready = func(ctx context.Context, chosen productlayout.Layout) error {
			if chosen.ExecutableDirectory == old.ExecutableDirectory {
				return WaitForService(ctx, oldIdentity, chosen)
			}
			return WaitForService(ctx, identity, chosen)
		}
	}
	if err := installationmanager.SaveServiceSwitch(ctx, state); err != nil {
		return before, err
	}
	actual, err := installationmanager.Service(ctx, "replace", layout, ready)
	if err != nil {
		if actual.Directory != before.Directory || actual.Enabled != before.Enabled || actual.Running != before.Running {
			return actual, errors.Join(installationmanager.ErrServiceRecovery, err)
		}
		if before.Running {
			if readyErr := ready(ctx, old); readyErr != nil {
				return actual, errors.Join(installationmanager.ErrServiceRecovery, err, readyErr)
			}
		}
	}
	if clearErr := installationmanager.ClearServiceSwitch(ctx); clearErr != nil {
		return actual, errors.Join(installationmanager.ErrServiceRecovery, err, clearErr)
	}
	actual.RecoveryPending = false
	actual.CanDiscardReplacement = false
	return actual, err
}

func recoverReplacement(ctx context.Context, state installationmanager.ServiceSwitch) (installationmanager.ServiceState, error) {
	var empty installationmanager.ServiceState
	if err := installationmanager.ReloadServiceSwitch(ctx); err != nil {
		return empty, err
	}
	current, found, err := installationmanager.RegisteredService()
	if err != nil || (found && current != state.Layout && current.ExecutableDirectory != state.TargetDirectory) {
		return empty, errors.Join(ErrModeConflict, err)
	}
	ready := func(ctx context.Context, layout productlayout.Layout) error {
		return WaitForService(ctx, Identity{ProductVersion: state.SourceVersion, BuildID: state.SourceBuild}, layout)
	}
	if found {
		if actual, err := installationmanager.Service(ctx, "stop", current, nil); err != nil {
			return actual, err
		}
	}
	if !found || current != state.Layout {
		if actual, err := installationmanager.Service(ctx, "replace", state.Layout, nil); err != nil {
			return actual, err
		}
	}
	enablement := "disable"
	if state.Enabled {
		enablement = "enable"
	}
	actual, err := installationmanager.Service(ctx, enablement, state.Layout, nil)
	if err != nil {
		return actual, err
	}
	if state.RuntimePID != 0 {
		actual, err = installationmanager.Service(ctx, "recover-start", state.Layout, ready)
		if err != nil {
			return actual, err
		}
	}
	err = installationmanager.ClearServiceSwitch(ctx)
	if err == nil {
		actual.RecoveryPending = false
		actual.CanDiscardReplacement = false
	}
	return actual, err
}

// Discard stops and removes only the recorded registration. It leaves both
// installations and their data in place and retires the journal last.
func discardReplacement(ctx context.Context, state installationmanager.ServiceSwitch) (installationmanager.ServiceState, error) {
	var empty installationmanager.ServiceState
	if err := installationmanager.ReloadServiceSwitch(ctx); err != nil {
		return empty, err
	}
	current, found, err := installationmanager.RegisteredService()
	if err != nil || (found && current != state.Layout && current.ExecutableDirectory != state.TargetDirectory) {
		return empty, errors.Join(ErrModeConflict, err)
	}
	layout := state.Layout
	if found {
		layout = current
	}
	actual, err := installationmanager.Service(ctx, "unregister", layout, nil)
	if err != nil || actual.Registered || actual.Running || actual.Enabled {
		return actual, errors.Join(installationmanager.ErrServiceRecovery, err)
	}
	err = installationmanager.ClearServiceSwitch(ctx)
	if err == nil {
		actual.RecoveryPending = false
		actual.CanDiscardReplacement = false
	}
	return actual, err
}

// CheckRPMServiceIPC is the read-only RPM recovery check for an observed main
// process. The caller also checks that systemd still reports that invocation.
