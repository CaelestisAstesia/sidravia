//go:build linux

package serviceswitch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"sidravia/internal/background/daemonctl"
	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/background/maintenance"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installationmanager"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/process/linuxproc"
)

func SwitchService(ctx context.Context, identity daemonctl.Identity, action string, owner int, interruptOneShot bool) (installationmanager.ServiceState, error) {
	var empty installationmanager.ServiceState
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil || !ns.IsProduction() {
		return empty, errors.Join(installationmanager.ErrServiceUnavailable, err)
	}
	layout, err := productlayout.ResolveNamespace(ns)
	if err != nil {
		return empty, err
	}
	ctx, unlock, err := installationmanager.BeginServiceSwitch(ctx)
	if err != nil {
		return empty, err
	}
	defer unlock()
	state, pending, err := installationmanager.PendingServiceSwitch()
	if err != nil {
		if action == "discard-damaged" && errors.Is(err, installationmanager.ErrServiceJournalDamaged) {
			return installationmanager.Service(ctx, "discard-damaged", layout, nil)
		}
		return empty, errors.Join(installationmanager.ErrServiceJournal, err)
	}
	if action == "discard-damaged" {
		return empty, installationmanager.ErrServiceJournal
	}
	if pending && state.Mode == "replacement" {
		if state.Layout.ExecutableDirectory != layout.ExecutableDirectory && state.TargetDirectory != layout.ExecutableDirectory {
			return empty, installationmanager.ErrServiceRecovery
		}
		switch action {
		case "recover":
			return recoverReplacement(ctx, state)
		case "discard":
			return discardReplacement(ctx, state)
		default:
			return empty, installationmanager.ErrServiceRecovery
		}
	}
	if action == "discard" {
		return empty, installationmanager.ErrServiceRecovery
	}
	registered, found, err := installationmanager.RegisteredService()
	if err != nil {
		return empty, err
	}
	if found {
		layout.ServiceCacheRoot = registered.ServiceCacheRoot
		if registered != layout {
			return empty, daemonctl.ErrModeConflict
		}
	}
	service := func(ctx context.Context, action string) (installationmanager.ServiceState, error) {
		return installationmanager.Service(ctx, action, layout, func(ctx context.Context, layout productlayout.Layout) error {
			return daemonctl.WaitForService(ctx, identity, layout)
		})
	}
	if pending && action == "recover" {
		if err := installationmanager.ReloadServiceSwitch(ctx); err != nil {
			return empty, err
		}
	}
	before, err := service(ctx, "status")
	if err != nil {
		return before, err
	}
	probeCurrent := func(ctx context.Context, inSystemd bool) (daemonctl.ProbeResult, error) {
		if inSystemd {
			return daemonctl.ProbeRegisteredService(ctx, identity, layout)
		}
		return daemonctl.ProbeApplication(ctx, identity, ns)
	}
	stop := func(ctx context.Context, inSystemd bool, running daemonctl.ProbeResult) error {
		if running.State == daemonctl.ProbeIncompatible {
			return daemonctl.ErrIncompatibleGeneration
		}
		if running.State != daemonctl.ProbeReachable {
			if inSystemd {
				_, err := service(ctx, "stop")
				return err
			}
			return nil
		}
		proc, err := linuxproc.Observe(running.Info.PID)
		if err != nil {
			return err
		}
		defer proc.Close()
		if proc.Executable != filepath.Join(layout.ExecutableDirectory, "sidraviad") {
			return daemonctl.ErrModeConflict
		}
		if inSystemd {
			_, err = service(ctx, "stop")
		} else {
			peer, connErr := daemonctl.Connect(ctx, identity, running.Info)
			if connErr != nil {
				return connErr
			}
			response, requestErr := peer.Call(ctx, contract.MethodDaemonStop, json.RawMessage("{}"))
			_ = peer.Close()
			if requestErr == nil && !response.OK {
				return daemonctl.ErrModeConflict
			}
			// A fast shutdown can close the transport before the reply. The
			// pinned process and the startup lease still prove it is gone.
			err = requestErr
		}
		if inSystemd && err != nil {
			return err
		}
		wait, cancel := context.WithTimeout(ctx, 17*time.Second)
		defer cancel()
		if waitErr := proc.Wait(wait); waitErr != nil {
			return errors.Join(err, waitErr)
		}
		return nil
	}
	launchTarget := func(ctx context.Context, systemd bool, owner int, state installationmanager.ServiceSwitch, restoring bool) error {
		if systemd {
			action := "start"
			if restoring {
				action = "recover-start"
			}
			if _, err := service(ctx, action); err != nil {
				return err
			}
		} else if owner > 0 {
			start := state.TargetOwnerStart
			if restoring {
				start = state.OwnerStart
			}
			if _, err := daemonctl.BootstrapDesktopForServiceSwitch(identity, owner, ns, strconv.FormatUint(start, 10)); err != nil {
				return err
			}
		} else if _, err := daemonctl.EnsureHeadless(identity, "info"); err != nil {
			return err
		}
		probe, err := probeCurrent(ctx, systemd)
		if err != nil || probe.State != daemonctl.ProbeReachable {
			return errors.Join(daemonctl.ErrConnectionUnavailable, err)
		}
		expected := launchcontract.ModeHeadless
		if systemd {
			expected = launchcontract.ModeSystemd
		} else if owner > 0 {
			expected = launchcontract.ModeDesktop
		}
		if !daemonctl.MatchesMode(probe.Status, expected, owner) {
			return daemonctl.ErrModeConflict
		}
		if state.ConfigurationID != "" && time.Now().Before(state.Expires) {
			peer, err := daemonctl.Connect(ctx, identity, probe.Info)
			if err != nil {
				return err
			}
			err = errors.Join(callServiceIPC(ctx, peer, contract.MethodUpdateResume, contract.UpdateResumePayload{
				OperationID: state.ID, ConfigurationID: state.ConfigurationID,
			}, nil), peer.Close())
			if err != nil {
				return err
			}
		}
		confirmed, err := probeCurrent(ctx, systemd)
		if err != nil || confirmed.State != daemonctl.ProbeReachable || confirmed.Info.PID != probe.Info.PID || confirmed.Info.Token != probe.Info.Token {
			return errors.Join(daemonctl.ErrConnectionUnavailable, err)
		}
		return installationmanager.ClearServiceSwitch(ctx)
	}
	takeover := action == "takeover"
	restore := func(ctx context.Context) error {
		_, current, err := installationmanager.RegisteredService()
		if err != nil {
			return err
		}
		observed, err := probeCurrent(ctx, current)
		if err != nil {
			return err
		}
		if !current && observed.State == daemonctl.ProbeReachable && observed.Info.PID == state.RuntimePID {
			peer, err := daemonctl.Connect(ctx, identity, observed.Info)
			if err != nil {
				return err
			}
			// If interruption happened before Commit, release the source's
			// preparation before requesting its normal stop.
			_ = callServiceIPC(ctx, peer, contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: state.ID}, nil)
			_ = peer.Close()
		}
		if err := stop(ctx, current, observed); err != nil {
			return err
		}
		originalWasSystemd := state.Mode == string(launchcontract.ModeSystemd)
		if current != originalWasSystemd {
			change := "unregister"
			if originalWasSystemd {
				change = "register"
			}
			if _, err := service(ctx, change); err != nil {
				return err
			}
		}
		if originalWasSystemd && state.Enabled {
			if _, err := service(ctx, "enable"); err != nil {
				return err
			}
		}
		originalOwner := state.OwnerPID
		if originalOwner != 0 {
			alive, err := linuxproc.GenerationRunning(originalOwner, strconv.FormatUint(state.OwnerStart, 10))
			if err != nil {
				return err
			}
			if !alive {
				// Do not resurrect a desktop process after its owner has left.
				return installationmanager.ClearServiceSwitch(ctx)
			}
		}
		return launchTarget(ctx, originalWasSystemd, originalOwner, state, true)
	}
	result := func(err error) (installationmanager.ServiceState, error) {
		statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
		defer cancel()
		actual, statusErr := service(statusCtx, "status")
		return actual, errors.Join(err, statusErr)
	}
	if pending {
		if action != "recover" || state.Layout.ExecutableDirectory != layout.ExecutableDirectory ||
			state.Layout.ConfigurationsPath != layout.ConfigurationsPath || state.Layout.RuntimeInfoPath != layout.RuntimeInfoPath {
			return before, installationmanager.ErrServiceRecovery
		}
		return result(restore(ctx))
	}
	if action == "recover" || takeover == before.Registered {
		return before, nil
	}
	observed, err := probeCurrent(ctx, before.Registered)
	if err != nil {
		return before, err
	}
	if observed.State == daemonctl.ProbeStopped {
		if before.Running {
			return before, daemonctl.ErrModeConflict
		}
		if takeover {
			return service(ctx, "register")
		}
		return service(ctx, "unregister")
	}
	if observed.State != daemonctl.ProbeReachable {
		return before, daemonctl.ErrModeConflict
	}
	proc, err := linuxproc.Observe(observed.Info.PID)
	if err != nil {
		return before, err
	}
	defer proc.Close()
	if proc.Executable != filepath.Join(layout.ExecutableDirectory, "sidraviad") {
		return before, daemonctl.ErrModeConflict
	}
	peer, err := daemonctl.Connect(ctx, identity, observed.Info)
	if err != nil {
		return before, err
	}
	defer peer.Close()
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return before, err
	}
	id := hex.EncodeToString(idBytes)
	var intent maintenance.View
	interactive := false
	err = callServiceIPC(ctx, peer, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{
		OperationID: id, CoordinatorPID: uint32(os.Getpid()), Interactive: &interactive,
		ServiceSwitch: true, InterruptOneShot: interruptOneShot,
	}, &intent)
	if err != nil {
		return before, err
	}
	operation := contract.UpdateOperationPayload{OperationID: id}
	prepared := true
	defer func() {
		if prepared {
			abort, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_ = callServiceIPC(abort, peer, contract.MethodUpdateAbort, operation, nil)
		}
	}()
	state = installationmanager.ServiceSwitch{
		ID: id, Layout: layout, Mode: observed.Status.Mode, ConfigurationID: intent.Intent.ConfigurationID,
		Enabled: before.Enabled, RuntimePID: observed.Info.PID, Expires: time.Now().Add(15 * time.Minute).UTC(),
	}
	if owner != 0 {
		targetOwner, err := linuxproc.Observe(owner)
		if err != nil {
			return before, err
		}
		state.TargetOwnerPID = owner
		state.TargetOwnerStart, err = strconv.ParseUint(targetOwner.Start, 10, 64)
		_ = targetOwner.Close()
		if err != nil {
			return before, err
		}
	}
	if observed.Status.DesktopOwnerPID != nil {
		state.OwnerPID = *observed.Status.DesktopOwnerPID
		ownerProc, err := linuxproc.Observe(state.OwnerPID)
		if err != nil {
			return before, err
		}
		state.OwnerStart, err = strconv.ParseUint(ownerProc.Start, 10, 64)
		_ = ownerProc.Close()
		if err != nil {
			return before, err
		}
	}
	if intent.Phase != "prepared" || intent.OperationID != id {
		return before, daemonctl.ErrModeConflict
	}
	if err := installationmanager.SaveServiceSwitch(ctx, state); err != nil {
		return before, err
	}
	perform := func() error {
		headless := observed.Status.Mode == string(launchcontract.ModeHeadless)
		if !before.Registered {
			err := callServiceIPC(ctx, peer, contract.MethodUpdateCommit, operation, nil)
			if err != nil && !headless {
				return err
			}
			prepared = false
			if headless {
				// Headless UpdateCommit already schedules the daemon's normal
				// shutdown. A second connection can race its closed listener.
				wait, cancel := context.WithTimeout(ctx, 17*time.Second)
				defer cancel()
				if waitErr := proc.Wait(wait); waitErr != nil {
					return errors.Join(err, waitErr)
				}
			}
		}
		if !headless {
			if err := stop(ctx, before.Registered, observed); err != nil {
				return err
			}
		}
		prepared = false
		change := "unregister"
		if takeover {
			change = "register"
		}
		if _, err := service(ctx, change); err != nil {
			return err
		}
		return launchTarget(ctx, takeover, owner, state, false)
	}
	if err := perform(); err != nil {
		if _, pending, readErr := installationmanager.PendingServiceSwitch(); !pending || readErr != nil {
			return result(errors.Join(err, readErr))
		}
		// Leave the durable record if undo is unsafe or fails. Explicit recover
		// will use the same bound original generation and launch policy.
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		return result(errors.Join(err, restore(restoreCtx)))
	}
	return result(nil)
}

func callServiceIPC(ctx context.Context, client daemonctl.Client, method string, value, result any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	response, err := client.Call(ctx, method, data)
	if err != nil {
		return err
	}
	if !response.OK {
		if response.Error != nil {
			return errors.New(response.Error.Code)
		}
		return daemonctl.ErrConnectionUnavailable
	}
	if result != nil {
		return json.Unmarshal(response.Result, result)
	}
	return nil
}
