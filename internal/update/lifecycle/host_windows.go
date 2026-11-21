//go:build windows

// Package lifecycle is the bounded CTL host for application-owned update
// preparation. It does not download, replace files or infer installer success.
package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/windowsplatform"
)

type processID struct {
	PID     uint32 `json:"pid"`
	Created uint64 `json:"created"`
}
type record struct {
	SchemaVersion int                  `json:"schemaVersion"`
	OperationID   string               `json:"operationId"`
	Source        update.Target        `json:"source"`
	Namespace     string               `json:"namespace"`
	Coordinator   processID            `json:"coordinator"`
	Daemon        processID            `json:"daemon"`
	GUI           processID            `json:"gui"`
	Intent        update.RestoreIntent `json:"intent"`
	Hashes        map[string]string    `json:"hashes"`
	Expires       time.Time            `json:"expires"`
	Stage         string               `json:"stage"`
	LogLevel      string               `json:"logLevel"`
	RecoveryToken string               `json:"recoveryToken,omitempty"`
}

func Run(ctx context.Context, action, op string, coordinator uint32, interactive bool, identity clientbootstrap.Identity) error {
	if !update.ValidOperationID(op) || coordinator == 0 {
		return update.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	peer, err := windowsplatform.Observe(coordinator)
	if err != nil {
		return err
	}
	defer peer.Close()
	parent, err := windowsplatform.ObserveParent()
	if err != nil {
		return err
	}
	defer parent.Close()
	if parent.PID != peer.PID || parent.Created != peer.Created || parent.SID != peer.SID {
		return windowsplatform.ErrPeer
	}
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return err
	}
	layout, err := productlayout.ResolveNamespace(ns)
	if err != nil {
		return err
	}
	source := update.Target{ID: layout.ExecutableDirectory, Version: identity.ProductVersion, BuildID: identity.BuildID, Layout: string(layout.Mode)}
	root, err := operations.DefaultRoot()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(layout.ExecutableDirectory) + "\x00" + ns.String()))
	directory := filepath.Join(root, hex.EncodeToString(sum[:16]), op)
	statePath := filepath.Join(directory, "state.json")
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		return err
	}
	raw, exists, err := store.Read(ctx, statePath, 16<<10)
	if err != nil {
		return err
	}
	var state record
	save := func() error {
		b, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return store.Replace(ctx, statePath, b)
	}
	if action == "prepare" {
		if exists {
			return update.ErrInvalid
		}
		state = record{SchemaVersion: 1, OperationID: op, Source: source, Namespace: ns.String(), Coordinator: processID{peer.PID, peer.Created}, Hashes: map[string]string{}, Expires: time.Now().Add(30 * time.Minute), Stage: "preparing", LogLevel: "debug", Intent: update.RestoreIntent{Mode: "none"}}
		for _, name := range []string{"sidraviactl.exe", "sidraviad.exe", "Sidravia.exe"} {
			path := filepath.Join(source.ID, name)
			if _, e := os.Lstat(path); errors.Is(e, os.ErrNotExist) && name == "Sidravia.exe" {
				continue
			}
			info, e := os.Lstat(path)
			if e != nil || !info.Mode().IsRegular() {
				return update.ErrInvalid
			}
			hash, e := windowsplatform.FileHash(path)
			if e != nil {
				return e
			}
			state.Hashes[name] = hash
		}
		probe, e := clientbootstrap.Probe(identity)
		if e != nil {
			return e
		}
		if probe.State == clientbootstrap.ProbeStopped {
			state.Stage = "prepared"
			return save()
		}
		if probe.State != clientbootstrap.ProbeReachable || probe.Status == nil {
			return update.ErrInvalid
		}
		daemon, e := observeTarget(uint32(probe.Status.PID), 0, source.ID, "sidraviad.exe", peer.SID)
		if e != nil {
			return e
		}
		defer daemon.Close()
		state.Daemon = processID{daemon.PID, daemon.Created}
		state.LogLevel = probe.Status.LogLevel
		if state.LogLevel == "" {
			state.LogLevel = "debug"
		}
		if probe.Status.Mode == "desktop" {
			if probe.Status.DesktopOwnerPID == nil {
				return update.ErrInvalid
			}
			gui, e := observeTarget(uint32(*probe.Status.DesktopOwnerPID), 0, source.ID, "Sidravia.exe", peer.SID)
			if e != nil {
				return e
			}
			defer gui.Close()
			state.GUI = processID{gui.PID, gui.Created}
		}
		conn, e := clientbootstrap.Connect(ctx, identity, probe.Info)
		if e != nil {
			return e
		}
		defer conn.Close()
		// Save before requesting preparation so any interrupted host has a fixed,
		// coordinator-bound recovery record rather than an untracked application.
		if e = save(); e != nil {
			return e
		}
		input := contract.UpdatePreparePayload{OperationID: op, CoordinatorPID: coordinator, Interactive: &interactive}
		view, e := call(ctx, conn, contract.MethodUpdatePrepare, input)
		if e != nil {
			return e
		}
		for view.Phase == "requested" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
			view, e = call(ctx, conn, contract.MethodUpdateStatus, struct{}{})
			if e != nil {
				return e
			}
		}
		if view.OperationID != op || view.Phase != "prepared" {
			return update.ErrInvalid
		}
		state.Intent = view.Intent
		state.Stage = "prepared"
		return save()
	}
	if action == "abort" && !exists {
		return nil
	}
	if !exists || jsonfile.DecodeStrict(raw, &state) != nil || state.SchemaVersion != 1 || state.OperationID != op || state.Source != source || state.Namespace != ns.String() || state.Coordinator != (processID{peer.PID, peer.Created}) || !time.Now().Before(state.Expires) {
		return update.ErrInvalid
	}
	if err = verifySource(state); err != nil {
		return err
	}
	if action == "stop" {
		if state.Stage != "prepared" {
			return update.ErrInvalid
		}
		if state.Daemon.PID == 0 {
			state.Stage = "stopped"
			return save()
		}
		daemon, e := observeTarget(state.Daemon.PID, state.Daemon.Created, source.ID, "sidraviad.exe", peer.SID)
		if e != nil {
			return e
		}
		defer daemon.Close()
		var gui *windowsplatform.Process
		if state.GUI.PID != 0 {
			gui, e = observeTarget(state.GUI.PID, state.GUI.Created, source.ID, "Sidravia.exe", peer.SID)
			if e != nil {
				return e
			}
			defer gui.Close()
		}
		probe, e := clientbootstrap.Probe(identity)
		if e != nil || probe.Info.PID != int(daemon.PID) {
			return update.ErrInvalid
		}
		conn, e := clientbootstrap.Connect(ctx, identity, probe.Info)
		if e != nil {
			return e
		}
		defer conn.Close()
		state.Stage = "committing"
		if e = save(); e != nil {
			return e
		}
		if _, e = call(ctx, conn, contract.MethodUpdateCommit, contract.UpdateOperationPayload{OperationID: op}); e != nil {
			return e
		}
		wait, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		if gui != nil {
			if e = gui.Wait(wait); e != nil {
				return e
			}
		}
		if e = daemon.Wait(wait); e != nil {
			return e
		}
		state.Stage = "stopped"
		return save()
	}
	if action != "abort" {
		return update.ErrInvalid
	}
	if state.Stage == "aborted" {
		return nil
	}
	if state.Stage == "preparing" || state.Stage == "prepared" {
		if state.Daemon.PID != 0 {
			probe, e := clientbootstrap.Probe(identity)
			if e != nil {
				return e
			}
			if probe.State != clientbootstrap.ProbeReachable || probe.Info.PID != int(state.Daemon.PID) {
				return update.ErrInvalid
			}
			conn, e := clientbootstrap.Connect(ctx, identity, probe.Info)
			if e != nil {
				return e
			}
			defer conn.Close()
			view, e := call(ctx, conn, contract.MethodUpdateStatus, struct{}{})
			if e != nil {
				return e
			}
			if view.Phase != "idle" {
				if _, e = call(ctx, conn, contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: op}); e != nil {
					return e
				}
			}
		}
		state.Stage = "aborted"
		return save()
	}
	if state.Stage != "stopped" && state.Stage != "committing" {
		return update.ErrInvalid
	}
	// A committed-but-incomplete stop must not start a duplicate or override a
	// surviving generation. This recovery path is only for unchanged old bytes.
	for _, previous := range []processID{state.Daemon, state.GUI} {
		if previous.PID == 0 {
			continue
		}
		running, e := windowsplatform.GenerationRunning(previous.PID, previous.Created)
		if e != nil {
			return e
		}
		if running {
			return update.ErrInvalid
		}
	}
	if err = restore(ctx, state, identity, peer.SID, directory); err != nil {
		return err
	}

	state.Stage = "aborted"
	return save()
}
func call(ctx context.Context, conn clientbootstrap.Client, method string, value any) (update.MaintenanceView, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return update.MaintenanceView{}, e
	}
	result, e := conn.Call(ctx, method, b)
	if e != nil {
		return update.MaintenanceView{}, e
	}
	if !result.OK {
		return update.MaintenanceView{}, update.ErrInvalid
	}
	var view update.MaintenanceView
	if e = jsonfile.DecodeStrict(result.Result, &view); e != nil || view.SchemaVersion != 1 {
		return view, update.ErrInvalid
	}
	return view, nil
}
func verifySource(state record) error {
	for name, hash := range state.Hashes {
		if name != "sidraviactl.exe" && name != "sidraviad.exe" && name != "Sidravia.exe" {
			return update.ErrInvalid
		}
		info, e := os.Lstat(filepath.Join(state.Source.ID, name))
		if e != nil || !info.Mode().IsRegular() {
			return update.ErrInvalid
		}
		actual, e := windowsplatform.FileHash(filepath.Join(state.Source.ID, name))
		if e != nil || actual != hash {
			return update.ErrInvalid
		}
	}
	if len(state.Hashes) < 2 {
		return update.ErrInvalid
	}
	return nil
}
func observeTarget(pid uint32, created uint64, directory, name, sid string) (*windowsplatform.Process, error) {
	p, e := windowsplatform.Observe(pid)
	if e != nil {
		return nil, e
	}
	if (created != 0 && p.Created != created) || !strings.EqualFold(p.Path, filepath.Join(directory, name)) || p.SID != sid || !p.Running() {
		p.Close()
		return nil, fmt.Errorf("target process identity: %w", update.ErrInvalid)
	}
	return p, nil
}

// PreparedSnapshot exports only the application-owned saved-config intent to
// the signed setup coordinator. It never exports runtime tokens or credentials.
func PreparedSnapshot(ctx context.Context, op string, coordinator uint32, identity clientbootstrap.Identity) (update.RestoreIntent, string, error) {
	parent, err := windowsplatform.ObserveParent()
	if err != nil {
		return update.RestoreIntent{}, "", err
	}
	defer parent.Close()
	if parent.PID != coordinator || !parent.Running() {
		return update.RestoreIntent{}, "", update.ErrInvalid
	}
	ns, err := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return update.RestoreIntent{}, "", err
	}
	layout, err := productlayout.ResolveNamespace(ns)
	if err != nil {
		return update.RestoreIntent{}, "", err
	}
	root, err := operations.DefaultRoot()
	if err != nil {
		return update.RestoreIntent{}, "", err
	}
	hash := sha256.Sum256([]byte(strings.ToLower(layout.ExecutableDirectory) + "\x00" + ns.String()))
	path := filepath.Join(root, hex.EncodeToString(hash[:16]), op, "state.json")
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		return update.RestoreIntent{}, "", err
	}
	data, exists, err := store.Read(ctx, path, 16<<10)
	if err != nil {
		return update.RestoreIntent{}, "", err
	}
	var state record
	expected := update.Target{ID: layout.ExecutableDirectory, Version: identity.ProductVersion, BuildID: identity.BuildID, Layout: string(layout.Mode)}
	if !exists || jsonfile.DecodeStrict(data, &state) != nil || state.SchemaVersion != 1 || state.OperationID != op || state.Source != expected || state.Namespace != ns.String() || state.Coordinator != (processID{parent.PID, parent.Created}) || state.Stage != "prepared" || !time.Now().Before(state.Expires) {
		return update.RestoreIntent{}, "", update.ErrInvalid
	}
	if err := verifySource(state); err != nil {
		return update.RestoreIntent{}, "", err
	}
	return state.Intent, state.LogLevel, nil
}

// RestoreInstalled is called only after the signed coordinator has verified the
// complete new installation and claimed its durable Resuming transition.
func RestoreInstalled(ctx context.Context, op string, target update.Target, intent update.RestoreIntent, expires time.Time, level, directory string, identity clientbootstrap.Identity, sid, nonce string) error {
	state := record{OperationID: op, Source: target, Intent: intent, Expires: expires, LogLevel: level, RecoveryToken: nonce}
	return restore(ctx, state, identity, sid, directory)
}
func restore(ctx context.Context, state record, identity clientbootstrap.Identity, sid, directory string) error {
	op := state.OperationID
	source := state.Source
	ticket := update.ResumeTicket{OperationID: op, Target: source, ExpiresAt: state.Expires, Intent: state.Intent}
	if _, err := update.ClaimRestore(directory, ticket, source, true, time.Now()); err != nil {
		return err
	}
	var restored contract.RuntimeInfo
	if state.Intent.Mode == "headless" {
		var e error
		if state.RecoveryToken != "" {
			restored, e = clientbootstrap.StartHeadlessForUpdate(identity, state.LogLevel, op, state.RecoveryToken, state.Intent.ConfigurationID)
		} else {
			restored, e = clientbootstrap.StartHeadlessForRecovery(identity, state.LogLevel)
		}
		if e != nil {
			return e
		}
	} else if state.Intent.Mode == "desktop" {
		args := []string{"--skip-auto-login", "--start-visible"}
		if !state.Intent.Visible {
			args = []string{"--skip-auto-login", "--start-hidden"}
		}
		env := os.Environ()
		if state.RecoveryToken != "" {
			clean := env[:0]
			for _, item := range env {
				name, _, _ := strings.Cut(item, "=")
				if !strings.EqualFold(name, launchcontract.EnvRecoveryOperation) && !strings.EqualFold(name, launchcontract.EnvRecoveryToken) && !strings.EqualFold(name, launchcontract.EnvRecoveryConfiguration) {
					clean = append(clean, item)
				}
			}
			env = append(clean, launchcontract.EnvRecoveryOperation+"="+op, launchcontract.EnvRecoveryToken+"="+state.RecoveryToken, launchcontract.EnvRecoveryConfiguration+"="+state.Intent.ConfigurationID)
		}
		cmd, e := windowsplatform.StartApplicationEnvironment(filepath.Join(source.ID, "Sidravia.exe"), args, env)
		if e != nil {
			return e
		}
		gui, e := observeTarget(uint32(cmd.Process.Pid), 0, source.ID, "Sidravia.exe", sid)
		if e != nil {
			cmd.Process.Release()
			return e
		}
		defer gui.Close()
		pid := cmd.Process.Pid
		if e = cmd.Process.Release(); e != nil {
			return e
		}
		ready := false
		for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
			probe, e := clientbootstrap.Probe(identity)
			if e == nil && probe.State == clientbootstrap.ProbeReachable && probe.Status != nil && probe.Status.DesktopOwnerPID != nil && *probe.Status.DesktopOwnerPID == pid && gui.Running() {
				restored = probe.Info
				ready = true
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		if !ready {
			return update.ErrInvalid
		}
	}
	if state.Intent.Maintain {
		probe, e := clientbootstrap.Probe(identity)
		if e != nil || probe.State != clientbootstrap.ProbeReachable || probe.Info != restored {
			return update.ErrInvalid
		}
		conn, e := clientbootstrap.Connect(ctx, identity, probe.Info)
		if e != nil {
			return e
		}
		defer conn.Close()
		payload, _ := json.Marshal(contract.UpdateResumePayload{OperationID: op, ConfigurationID: state.Intent.ConfigurationID})
		result, e := conn.Call(ctx, contract.MethodUpdateResume, payload)
		if e != nil {
			return e
		}
		if !result.OK {
			return update.ErrInvalid
		}
	}
	return nil
}
