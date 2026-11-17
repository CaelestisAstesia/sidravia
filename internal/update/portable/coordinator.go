//go:build linux || windows

package portable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
	"sidravia/internal/update/operations"
)

var ErrPreparation = errors.New("application did not approve update preparation")

var ErrRestore = errors.New("updated files are installed but application recovery was not confirmed")

// Execution is a finite, already authenticated portable operation. The host
// retains check/download/user-consent ownership. ReleaseRuntime transfers only
// its own startup lease after Preparing is durable; it never stops another app.
type Execution struct {
	Store          *operations.Store
	OperationID    string
	Target         string
	Old            Inventory
	Payload        *Payload
	Namespace      string
	ReleaseRuntime func()
}
type applicationPeer struct {
	connection  *client.Client
	daemon, gui *applicationProcess
	info        contract.RuntimeInfo
	status      contract.StatusResult
}

func (p *applicationPeer) close() {
	if p == nil {
		return
	}
	if p.connection != nil {
		p.connection.Close()
	}
	if p.daemon != nil {
		p.daemon.Close()
	}
	if p.gui != nil {
		p.gui.Close()
	}
}
func call(ctx context.Context, c *client.Client, method string, input any, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := c.Call(bounded, method, data)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return &peerFailure{response.Error}
	}
	if output != nil {
		return json.Unmarshal(response.Result, output)
	}
	return nil
}
func probeApplication(ctx context.Context, layout productlayout.Layout, identity update.Target) (peer *applicationPeer, err error) {
	file, err := os.Open(layout.RuntimeInfoPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	closeErr := file.Close()
	err = errors.Join(err, closeErr)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, ErrInvalid
	}
	info, err := contract.DecodeRuntimeInfo(data)
	if err != nil {
		return nil, err
	}
	if info.ProductVersion != identity.Version || info.BuildID != identity.BuildID {
		return nil, ErrChanged
	}
	p := &applicationPeer{info: info}
	defer func() {
		if err != nil {
			p.close()
		}
	}()
	p.daemon, err = observeExecutable(info.PID, filepath.Join(identity.ID, daemonName()))
	if err != nil {
		return nil, err
	}
	p.connection, err = client.Connect(ctx, info.Endpoint, info.Token, identity.BuildID)
	if err != nil {
		return nil, err
	}
	if err = call(ctx, p.connection, contract.MethodDaemonStatus, struct{}{}, &p.status); err != nil {
		return nil, err
	}
	if p.status.PID != info.PID || p.status.ProductVersion != identity.Version || p.status.BuildID != identity.BuildID {
		return nil, ErrChanged
	}
	if p.status.Mode == "desktop" {
		if p.status.DesktopOwnerPID == nil {
			return nil, ErrChanged
		}
		p.gui, err = observeExecutable(*p.status.DesktopOwnerPID, guiRuntime(identity.ID))
		if err != nil {
			return nil, err
		}
	} else if p.status.Mode != "headless" || p.status.DesktopOwnerPID != nil {
		return nil, ErrChanged
	}
	return p, nil
}
func waitPeers(ctx context.Context, p *applicationPeer) error {
	if p == nil {
		return nil
	}
	if p.gui != nil {
		if err := p.gui.Wait(ctx); err != nil {
			return err
		}
	}
	return p.daemon.Wait(ctx)
}

// Execute negotiates normal exit, applies bytes and restores at most one cold
// generation. No process is forcibly killed and no authentication secret enters
// its record. An absent original application remains absent.
func Execute(ctx context.Context, input Execution) (resultErr error) {
	if input.Store == nil {
		return ErrInvalid
	}
	release, err := input.Store.AcquireCoordinator(ctx, input.OperationID)
	if err != nil {
		return err
	}
	defer release()
	record, exists, err := input.Store.Read(ctx)
	if err != nil {
		return err
	}
	if !exists || !boundExecution(record, input.Target, input.Old, input.Payload) {
		return ErrInvalid
	}
	ns, err := productlayout.NewNamespace(input.Namespace)
	if err != nil {
		return err
	}
	layout, err := productlayout.ResolveTarget(input.Target, ns)
	if err != nil || layout.Mode != productlayout.ModePortable {
		return errors.Join(ErrInvalid, err)
	}
	if err := Preflight(ctx, input.Target, input.Old, input.Payload); err != nil {
		return err
	}
	peer, err := probeApplication(ctx, layout, record.Source)
	if err != nil {
		return err
	}
	defer peer.close()
	owner, err := observeProcess(os.Getpid())
	if err != nil {
		return err
	}
	defer owner.Close()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	recovery := operations.Recovery{SourceFilesSHA256: digest(input.Old.data), Intent: update.RestoreIntent{Mode: "none"}, Namespace: input.Namespace, CoordinatorPID: owner.PID, CoordinatorStart: owner.Start, Nonce: hex.EncodeToString(nonce), ExpiresAt: time.Now().Add(30 * time.Minute), LogLevel: "info"}
	prepared := false
	committed := false
	// Before commit, cancelling preparation thaws the live generation. This
	// deferred cleanup never tries to resurrect an operation that began writing.
	defer func() {
		if prepared && peer != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, call(cleanup, peer.connection, contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: record.ID}, nil))
		}
	}()
	if peer != nil {
		interactive := false
		var view update.MaintenanceView
		prepared = true
		if err := call(ctx, peer.connection, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{OperationID: record.ID, CoordinatorPID: uint32(owner.PID), Interactive: &interactive}, &view); err != nil {
			return err
		}
		prepared = true
		wait, cancel := context.WithTimeout(ctx, 15*time.Second)
		for view.Phase == "requested" && wait.Err() == nil {
			if err := pause(wait); err != nil {
				cancel()
				return err
			}
			if err := call(wait, peer.connection, contract.MethodUpdateStatus, struct{}{}, &view); err != nil {
				cancel()
				return err
			}
		}
		cancel()
		if view.OperationID != record.ID || view.Phase != "prepared" {
			return ErrPreparation
		}
		recovery.Intent = view.Intent
		recovery.LogLevel = peer.status.LogLevel
		if recovery.LogLevel == "" {
			recovery.LogLevel = "info"
		}
	}
	abort := func(cause error) error {
		cleanup, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		current, _, readErr := input.Store.Read(cleanup)
		if readErr != nil {
			return errors.Join(cause, readErr)
		}
		if current.Phase != operations.Verified && current.Phase != operations.Preparing && current.Phase != operations.Quiescent {
			return cause
		}
		if _, err := os.Lstat(filepath.Join(input.Target, operations.StartupSeal)); !os.IsNotExist(err) {
			return cause
		}
		root, openErr := os.OpenRoot(input.Target)
		if openErr != nil {
			return errors.Join(cause, openErr)
		}
		checkErr := verifyInventory(cleanup, root, input.Old)
		if checkErr == nil {
			checkErr = verifyGUI(root, input.Old)
		}
		root.Close()
		if checkErr != nil {
			return errors.Join(cause, checkErr)
		}
		if prepared && peer != nil {
			if e := call(cleanup, peer.connection, contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: record.ID}, nil); e == nil {
				prepared = false
			}
		}
		_, changed, e := input.Store.Advance(cleanup, record.ID, current.Phase, operations.Aborted, "preparation_failed", time.Now())
		if e != nil || !changed {
			return errors.Join(cause, e)
		}
		if committed && peer != nil {
			wait, stop := context.WithTimeout(cleanup, 15*time.Second)
			e := waitPeers(wait, peer)
			stop()
			if e != nil {
				return errors.Join(cause, e)
			}
		}
		if peer != nil {
			daemonLive, e := peer.daemon.Running()
			if e != nil {
				return errors.Join(cause, e)
			}
			guiLive := false
			if peer.gui != nil {
				guiLive, e = peer.gui.Running()
				if e != nil {
					return errors.Join(cause, e)
				}
			}
			if daemonLive || guiLive {
				return cause
			} // never adopt or duplicate a survivor
		}
		// The fresh Aborted transition is a one-time recovery claim for unchanged
		// source bytes. Replaying Execute cannot obtain coordinator admission again.
		_, e = restoreApplication(cleanup, layout, record.Source, recovery, "", "")
		return errors.Join(cause, e)
	}
	if err := input.Store.SetRecovery(ctx, record.ID, recovery, time.Now()); err != nil {
		return abort(err)
	}
	if _, _, err := input.Store.Advance(ctx, record.ID, operations.Verified, operations.Preparing, "", time.Now()); err != nil {
		return abort(err)
	}
	if input.ReleaseRuntime != nil {
		input.ReleaseRuntime()
	}
	if err := ctx.Err(); err != nil {
		return abort(err)
	}
	if peer != nil {
		committed = true
		if err := call(ctx, peer.connection, contract.MethodUpdateCommit, contract.UpdateOperationPayload{OperationID: record.ID}, nil); err != nil {
			return abort(err)
		}
		prepared = false
		wait, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := waitPeers(wait, peer)
		cancel()
		if err != nil {
			return abort(err)
		}
	}
	if _, _, err := input.Store.Advance(ctx, record.ID, operations.Preparing, operations.Quiescent, "", time.Now()); err != nil {
		return abort(err)
	}
	if err := Apply(ctx, input.Store, record.ID, input.Target, input.Old, input.Payload); err != nil {
		return abort(err)
	}
	// Cancellation after writing starts cannot skip bounded recovery accounting.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	if recovery.Intent.Mode == "none" {
		_, _, err := input.Store.Advance(work, record.ID, operations.Applied, operations.Completed, "", time.Now())
		if err != nil {
			return err
		}
		return finishApplication(input.Target, record.ID)
	}
	if _, changed, err := input.Store.Advance(work, record.ID, operations.Applied, operations.Resuming, "", time.Now()); err != nil || !changed {
		return errors.Join(operations.ErrConflict, err)
	}
	_, err = restoreApplication(work, layout, record.Target, recovery, record.ID, recovery.Nonce)
	if err != nil {
		_, _, stateErr := input.Store.Advance(work, record.ID, operations.Resuming, operations.ResumeFailed, "resume_failed", time.Now())
		if stateErr != nil {
			return errors.Join(ErrRestore, err, stateErr)
		}
		return errors.Join(ErrRestore, err, finishApplication(input.Target, record.ID))
	}
	_, _, err = input.Store.Advance(work, record.ID, operations.Resuming, operations.Completed, "", time.Now())
	if err != nil {
		return err
	}
	return finishApplication(input.Target, record.ID)
}
func pause(ctx context.Context) error {
	timer := time.NewTimer(75 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type peerFailure struct{ reply *contract.Error }

func (e *peerFailure) Error() string { return "update lifecycle peer rejected the request" }
