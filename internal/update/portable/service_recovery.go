//go:build linux || windows

package portable

import (
	"context"
	"errors"
	"time"

	"sidravia/internal/installationmanager"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
	"sidravia/internal/update/operations"
)

// RecoverManagedService resumes an interrupted product rollback. Both indexes
// are revalidated independently of the installed tree before it is touched.
func RecoverManagedService(ctx context.Context, store *operations.Store, id, target string) error {
	if store == nil || !canonical(target) {
		return ErrInvalid
	}
	releaseCoordinator, err := store.AcquireRollbackCoordinator(ctx, id)
	if err != nil {
		return err
	}
	defer releaseCoordinator()
	record, _, err := store.Read(ctx)
	if err != nil || record.Source.ID != target {
		return errors.Join(ErrChanged, err)
	}
	if record.Phase == operations.Completed || record.Phase == operations.Aborted {
		return cleanRollbackSnapshot(record, nil)
	}
	return installationmanager.WithServiceRegistration(ctx, func(lease *installationmanager.RegistrationLease) error {
		return recoverManagedServiceWithRegistration(ctx, lease, store, id, target, record)
	})
}

func recoverManagedServiceWithRegistration(ctx context.Context, lease *installationmanager.RegistrationLease, store *operations.Store, id, target string, record operations.Record) (resultErr error) {
	layout, found, err := installationmanager.RegisteredService()
	if err != nil || !found || layout.ExecutableDirectory != target || layout.Mode != productlayout.ModePortable {
		return errors.Join(ErrChanged, err)
	}
	if record.Phase == operations.RolledBack {
		pending, err := ManagedRollbackPending(target, id)
		if err != nil || !pending {
			return err
		}
		// The old tree was authenticated before publishing RolledBack.
		// Cleanup can already have removed the new index and backup files.
		if record.Recovery.Intent.Mode == "systemd" {
			if err := startManagedService(ctx, lease, layout, record.Source, true); err != nil {
				return err
			}
		}
		return cleanRollbackSnapshot(record, nil)
	}
	backup, next, dirs, err := loadRollback(ctx, store, record)
	if err != nil {
		return err
	}
	clean := false
	defer func() {
		if clean {
			resultErr = errors.Join(resultErr, backup.root.Close(), cleanRollbackSnapshot(record, &backup.inventory))
		} else {
			resultErr = errors.Join(resultErr, backup.root.Close())
		}
	}()
	if record.Phase != operations.RolledBack {
		if _, err := lease.Service(ctx, "stop", layout, nil); err != nil {
			return err
		}
		if record.Phase != operations.RollingBack {
			if _, changed, err := store.Advance(ctx, id, record.Phase, operations.RollingBack, "", time.Now()); err != nil || !changed {
				return errors.Join(operations.ErrConflict, err)
			}
		}
		input := Execution{Store: store, OperationID: id, Target: target, Old: backup.inventory, Payload: &Payload{inventory: next}}
		if err := restoreServiceFiles(ctx, input, backup, dirs); err != nil {
			return err
		}
	}
	if record.Recovery.Intent.Mode == "systemd" {
		if err := startManagedService(ctx, lease, layout, record.Source, true); err != nil {
			return err
		}
	}
	clean = true
	return nil
}

func resumeManagedPreparation(ctx context.Context, store *operations.Store, record operations.Record, start func(context.Context, update.Target) error) error {
	if record.Phase != operations.SourceStarting {
		if _, changed, err := store.Advance(ctx, record.ID, record.Phase, operations.SourceStarting, "", time.Now()); err != nil || !changed {
			return errors.Join(operations.ErrConflict, err)
		}
	}
	if record.Recovery.Intent.Mode == "systemd" {
		if err := start(ctx, record.Source); err != nil {
			return err
		}
	}
	_, changed, err := store.Advance(ctx, record.ID, operations.SourceStarting, operations.Aborted, "preparation_failed", time.Now())
	if !changed {
		return errors.Join(operations.ErrConflict, err)
	}
	return err
}
