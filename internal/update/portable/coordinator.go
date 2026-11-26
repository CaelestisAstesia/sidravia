//go:build linux || windows

package portable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/update/portable/internal/filetxn"
	"time"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/installationmanager"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/transaction"
)

var ErrPreparation = errors.New("application did not approve update preparation")

var ErrRestore = errors.New("updated files are installed but application recovery was not confirmed")

// execution is a finite, already authenticated portable operation. The host
// retains check/download/user-consent ownership. ReleaseRuntime transfers only
// its own startup lease after Preparing is durable; it never stops another app.
type execution struct {
	Store          *transaction.Store
	OperationID    string
	Target         string
	Old            filetxn.Inventory
	Payload        *filetxn.Payload
	Namespace      string
	ReleaseRuntime func()
}

// Execute negotiates normal exit, applies bytes and restores at most one cold
// generation. No process is forcibly killed and no authentication secret enters
// its record. An absent original application remains absent.
func execute(ctx context.Context, input execution) (resultErr error) {
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
	if !exists || !filetxn.BoundExecution(record, input.Target, input.Old, input.Payload) {
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
	if err := filetxn.Preflight(ctx, input.Target, input.Old, input.Payload); err != nil {
		return err
	}
	peer, err := probeApplication(ctx, layout, record.Source)
	if err != nil {
		return err
	}
	defer peer.close()
	if ns.IsProduction() {
		registered, found, err := installationmanager.RegisteredService()
		if err != nil {
			return err
		}
		if found && registered.ExecutableDirectory == layout.ExecutableDirectory {
			return executeManaged(ctx, input, record, layout, registered, peer)
		}
	}
	if peer != nil && peer.status.Mode == "systemd" {
		return ErrChanged
	}
	owner, err := observeProcess(os.Getpid())
	if err != nil {
		return err
	}
	defer owner.Close()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	recovery := transaction.Recovery{SourceFilesSHA256: input.Old.IndexSHA256(), Intent: maintenance.Intent{Mode: "none"}, Namespace: input.Namespace, CoordinatorPID: owner.PID, CoordinatorStart: owner.Start, Nonce: hex.EncodeToString(nonce), ExpiresAt: time.Now().Add(30 * time.Minute), LogLevel: "info"}
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
		var view maintenance.View
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
		if current.Phase != transaction.Verified && current.Phase != transaction.Preparing && current.Phase != transaction.Quiescent {
			return cause
		}
		if _, err := os.Lstat(filepath.Join(input.Target, transaction.StartupSeal)); !os.IsNotExist(err) {
			return cause
		}
		root, openErr := os.OpenRoot(input.Target)
		if openErr != nil {
			return errors.Join(cause, openErr)
		}
		checkErr := filetxn.VerifyInventory(cleanup, root, input.Old)
		if checkErr == nil {
			checkErr = filetxn.VerifyGUI(root, input.Old)
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
		_, changed, e := input.Store.Advance(cleanup, record.ID, current.Phase, transaction.Aborted, "preparation_failed", time.Now())
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
	if _, _, err := input.Store.Advance(ctx, record.ID, transaction.Verified, transaction.Preparing, "", time.Now()); err != nil {
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
	if _, _, err := input.Store.Advance(ctx, record.ID, transaction.Preparing, transaction.Quiescent, "", time.Now()); err != nil {
		return abort(err)
	}
	if err := filetxn.Apply(ctx, input.Store, record.ID, input.Target, input.Old, input.Payload); err != nil {
		return abort(err)
	}
	// Cancellation after writing starts cannot skip bounded recovery accounting.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	if recovery.Intent.Mode == "none" {
		_, _, err := input.Store.Advance(work, record.ID, transaction.Applied, transaction.Completed, "", time.Now())
		if err != nil {
			return err
		}
		return filetxn.RemoveSeal(input.Target, record.ID)
	}
	if _, changed, err := input.Store.Advance(work, record.ID, transaction.Applied, transaction.Resuming, "", time.Now()); err != nil || !changed {
		return errors.Join(transaction.ErrConflict, err)
	}
	_, err = restoreApplication(work, layout, record.Target, recovery, record.ID, recovery.Nonce)
	if err != nil {
		_, _, stateErr := input.Store.Advance(work, record.ID, transaction.Resuming, transaction.ResumeFailed, "resume_failed", time.Now())
		if stateErr != nil {
			return errors.Join(ErrRestore, err, stateErr)
		}
		return errors.Join(ErrRestore, err, filetxn.RemoveSeal(input.Target, record.ID))
	}
	_, _, err = input.Store.Advance(work, record.ID, transaction.Resuming, transaction.Completed, "", time.Now())
	if err != nil {
		return err
	}
	return filetxn.RemoveSeal(input.Target, record.ID)
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
