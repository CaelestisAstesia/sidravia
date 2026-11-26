//go:build linux || windows

package portable

import (
	"context"
	"errors"
	"sidravia/internal/update/portable/internal/filetxn"
	"time"

	"sidravia/internal/installation/change"
	"sidravia/internal/installationmanager"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/transaction"
)

// RecoverManagedService resumes an interrupted product rollback. Both indexes
// are revalidated independently of the installed tree before it is touched.
func RecoverManagedService(ctx context.Context, store *transaction.Store, id, target string) (resultErr error) {
	if store == nil || !filetxn.Canonical(target) {
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
	if record.Phase == transaction.Completed || record.Phase == transaction.Aborted {
		return filetxn.CleanRollbackSnapshot(record, nil)
	}
	locked, release, err := installationmanager.HoldServiceRegistration(ctx)
	if err != nil {
		return err
	}
	defer release()
	ctx = locked
	layout, found, err := installationmanager.RegisteredService()
	if err != nil || !found || layout.ExecutableDirectory != target || layout.Mode != productlayout.ModePortable {
		return errors.Join(ErrChanged, err)
	}
	if record.Phase == transaction.RolledBack {
		pending, err := filetxn.ManagedRollbackPending(target, id)
		if err != nil || !pending {
			return err
		}
		// The old tree was authenticated before publishing RolledBack.
		// Cleanup can already have removed the new index and backup files.
		if record.Recovery.Intent.Mode == "systemd" {
			if err := startManagedService(ctx, layout, record.Source, true); err != nil {
				return err
			}
		}
		return filetxn.CleanRollbackSnapshot(record, nil)
	}
	backup, err := filetxn.OpenRollback(ctx, store, record)
	if err != nil {
		return err
	}
	clean := false
	defer func() {
		if clean {
			resultErr = errors.Join(resultErr, backup.Retire(record))
		} else {
			resultErr = errors.Join(resultErr, backup.Close())
		}
	}()
	if record.Phase != transaction.RolledBack {
		if _, err := installationmanager.Service(ctx, "stop", layout, nil); err != nil {
			return err
		}
		if record.Phase != transaction.RollingBack {
			if _, changed, err := store.Advance(ctx, id, record.Phase, transaction.RollingBack, "", time.Now()); err != nil || !changed {
				return errors.Join(transaction.ErrConflict, err)
			}
		}
		if err := backup.Restore(ctx, store, record); err != nil {
			return err
		}
	}
	if record.Recovery.Intent.Mode == "systemd" {
		if err := startManagedService(ctx, layout, record.Source, true); err != nil {
			return err
		}
	}
	clean = true
	return nil
}

func resumeManagedPreparation(ctx context.Context, store *transaction.Store, record transaction.Record, start func(context.Context, change.Target) error) error {
	if record.Phase != transaction.SourceStarting {
		if _, changed, err := store.Advance(ctx, record.ID, record.Phase, transaction.SourceStarting, "", time.Now()); err != nil || !changed {
			return errors.Join(transaction.ErrConflict, err)
		}
	}
	if record.Recovery.Intent.Mode == "systemd" {
		if err := start(ctx, record.Source); err != nil {
			return err
		}
	}
	_, changed, err := store.Advance(ctx, record.ID, transaction.SourceStarting, transaction.Aborted, "preparation_failed", time.Now())
	if !changed {
		return errors.Join(transaction.ErrConflict, err)
	}
	return err
}
