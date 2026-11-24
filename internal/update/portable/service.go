//go:build linux || windows

package portable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"sidravia/internal/installationmanager"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
	"sidravia/internal/update/operations"
)

var ErrRolledBack = errors.New("managed portable update was rolled back")

func executeManaged(ctx context.Context, input Execution, record operations.Record, layout, registration productlayout.Layout, peer *applicationPeer) (resultErr error) {
	locked, release, err := installationmanager.HoldServiceRegistration(ctx)
	if err != nil {
		return err
	}
	defer release()
	ctx = locked
	actual, found, err := installationmanager.RegisteredService()
	if err != nil || !found || actual != registration {
		return errors.Join(ErrChanged, err)
	}
	cache, err := os.UserCacheDir()
	if err != nil || registration.Mode != productlayout.ModePortable ||
		registration.ServiceCacheRoot != filepath.Clean(cache) ||
		registration.RuntimeInfoPath != layout.RuntimeInfoPath {
		return errors.Join(ErrChanged, err)
	}
	service := func(ctx context.Context, action string, ready func(context.Context, productlayout.Layout) error) (installationmanager.ServiceState, error) {
		return installationmanager.Service(ctx, action, layout, ready)
	}
	before, err := service(ctx, "status", nil)
	if err != nil {
		return err
	}
	if (peer != nil) != before.Running || (peer != nil && peer.status.Mode != "systemd") {
		return ErrChanged
	}
	var gui *applicationProcess
	if input.Old.manifest.PayloadKind == "gui" {
		gui, err = observeManagedGUI(input.Target)
		if err != nil {
			return err
		}
		if gui != nil {
			defer gui.Close()
			if peer == nil {
				return ErrPreparation
			}
		}
	}
	start := func(ctx context.Context, target update.Target) error {
		return startManagedService(ctx, layout, target, target == record.Source)
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
	intent := update.RestoreIntent{Mode: "none"}
	if before.Running {
		intent.Mode = "systemd"
	}
	now := time.Now()
	recovery := operations.Recovery{
		SourceFilesSHA256: digest(input.Old.data), Intent: intent, CoordinatorPID: owner.PID, ManagedService: true,
		CoordinatorStart: owner.Start, Nonce: hex.EncodeToString(nonce), ExpiresAt: now.Add(25 * time.Minute), LogLevel: "info",
	}
	if err := input.Store.SetRecovery(ctx, record.ID, recovery, now); err != nil {
		return err
	}
	if _, _, err := input.Store.Advance(ctx, record.ID, operations.Verified, operations.Preparing, "", time.Now()); err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		recover, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
		defer cancel()
		current, _, err := input.Store.Read(recover)
		if err != nil || (current.Phase != operations.Preparing && current.Phase != operations.Quiescent) {
			resultErr = errors.Join(resultErr, err)
			return
		}
		if _, err := os.Lstat(filepath.Join(input.Target, operations.StartupSeal)); !os.IsNotExist(err) {
			return
		}
		root, err := os.OpenRoot(input.Target)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			return
		}
		err = errors.Join(verifyInventory(recover, root, input.Old), verifyGUI(root, input.Old), root.Close())
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			return
		}
		resultErr = errors.Join(resultErr, resumeManagedPreparation(recover, input.Store, current, start))
	}()
	if gui != nil {
		// Close the frontend while the old daemon can still carry the
		// acknowledgement and commit over the existing IPC connection.
		defer func() {
			if resultErr != nil {
				alive, _ := peer.daemon.Running()
				if alive {
					abort, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					resultErr = errors.Join(resultErr, call(abort, peer.connection, contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: record.ID}, nil))
				}
			}
		}()
		if err := closeManagedGUI(ctx, record.ID, owner.PID, peer, gui); err != nil {
			return err
		}
	}
	if input.ReleaseRuntime != nil {
		input.ReleaseRuntime()
	}
	if before.Running {
		if _, err := service(ctx, "stop", nil); err != nil {
			return err
		}
		if err := waitPeers(ctx, peer); err != nil {
			return err
		}
	}
	if _, _, err := input.Store.Advance(ctx, record.ID, operations.Preparing, operations.Quiescent, "", time.Now()); err != nil {
		return err
	}
	dirs, err := newProductDirectories(input.Target, input.Payload.inventory)
	if err != nil {
		return err
	}
	backup, err := stageServiceRollback(ctx, input.Target, input.Old, rollbackDirectory(input.Target, record.ID))
	if err != nil {
		return err
	}
	keepBackup := false
	defer func() {
		if keepBackup {
			resultErr = errors.Join(resultErr, backup.root.Close())
		} else {
			resultErr = errors.Join(resultErr, backup.root.Close(), cleanRollbackSnapshot(record, &input.Old))
		}
	}()
	if err := persistRollbackInputs(ctx, backup, input.Payload.inventory, dirs); err != nil {
		return err
	}
	rollback := func(cause error, phase operations.Phase) error {
		keepBackup = true
		recover, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		if _, changed, err := input.Store.Advance(recover, record.ID, phase, operations.RollingBack, "", time.Now()); err != nil || !changed {
			return errors.Join(cause, operations.ErrConflict, err)
		}
		if err := restoreServiceFiles(recover, input, backup, dirs); err != nil {
			return errors.Join(cause, err)
		}
		if before.Running {
			if err := start(recover, record.Source); err != nil {
				return errors.Join(cause, err)
			}
		}
		keepBackup = false
		return errors.Join(ErrRolledBack, cause)
	}
	if err := Apply(ctx, input.Store, record.ID, input.Target, input.Old, input.Payload); err != nil {
		after, _, observeErr := input.Store.Read(context.WithoutCancel(ctx))
		if observeErr == nil && (after.Phase == operations.RepairRequired || after.Phase == operations.Applying || after.Phase == operations.Applied) {
			return rollback(err, after.Phase)
		}
		keepBackup = observeErr != nil || after.Phase == operations.Applying
		return errors.Join(err, observeErr)
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	// The fully verified package can start once the execution gate is free.
	if !before.Running {
		_, _, err := input.Store.Advance(finish, record.ID, operations.Applied, operations.Completed, "", time.Now())
		if err != nil {
			keepBackup = true
		}
		return err
	}
	if _, changed, err := input.Store.Advance(finish, record.ID, operations.Applied, operations.ServiceStarting, "", time.Now()); err != nil || !changed {
		actual, _, readErr := input.Store.Read(finish)
		if readErr != nil || (actual.Phase != operations.Applied && actual.Phase != operations.ServiceStarting) {
			keepBackup = true
			return errors.Join(operations.ErrConflict, err, readErr)
		}
		return rollback(errors.Join(operations.ErrConflict, err), actual.Phase)
	}
	if err := start(finish, record.Target); err != nil {
		// The service operation stopped the failed generation. AcquireExecution
		// and ensureQuiescent will refuse rollback if it is still alive.
		return rollback(errors.Join(ErrRestore, err), operations.ServiceStarting)
	}
	_, _, err = input.Store.Advance(finish, record.ID, operations.ServiceStarting, operations.Completed, "", time.Now())
	if err != nil {
		keepBackup = true
	}
	return err
}

func startManagedService(ctx context.Context, layout productlayout.Layout, target update.Target, restore bool) error {
	action := "start"
	if restore {
		action = "recover-start"
	}
	_, err := installationmanager.Service(ctx, action, layout, func(ctx context.Context, layout productlayout.Layout) error {
		wait, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		for {
			observed, err := probeApplication(wait, layout, target)
			if err != nil {
				return err
			}
			if observed != nil {
				defer observed.close()
				if observed.status.Mode != "systemd" {
					return ErrChanged
				}
				return nil
			}
			if err := pause(wait); err != nil {
				return err
			}
		}
	})
	return err
}

func closeManagedGUI(ctx context.Context, operation string, pid int, peer *applicationPeer, gui *applicationProcess) error {
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	interactive := false
	var view update.MaintenanceView
	if err := call(wait, peer.connection, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{
		OperationID: operation, CoordinatorPID: uint32(pid), Interactive: &interactive, ManagedGUIUpdate: true,
	}, &view); err != nil {
		return errors.Join(ErrPreparation, err)
	}
	for view.OperationID == operation && view.Phase == "requested" {
		if err := pause(wait); err != nil {
			return errors.Join(ErrPreparation, err)
		}
		if err := call(wait, peer.connection, contract.MethodUpdateStatus, struct{}{}, &view); err != nil {
			return errors.Join(ErrPreparation, err)
		}
	}
	if view.OperationID != operation || view.Phase != "prepared" {
		return ErrPreparation
	}
	if err := call(wait, peer.connection, contract.MethodUpdateCommit, contract.UpdateOperationPayload{OperationID: operation}, nil); err != nil {
		return errors.Join(ErrPreparation, err)
	}
	if err := gui.Wait(wait); err != nil {
		return errors.Join(ErrPreparation, err)
	}
	return nil
}
