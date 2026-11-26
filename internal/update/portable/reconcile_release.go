//go:build linux || windows

package portable

import (
	"context"

	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/transaction"
)

func ReconcileRelease(ctx context.Context, store *transaction.Store, id, target string, installed ReleaseInput) error {
	inventory, err := installed.authenticate()
	if err != nil {
		return err
	}
	return reconcileInterrupted(ctx, store, id, target, inventory)
}

func ManagedRollbackPending(target, id string) (bool, error) {
	return filetxn.ManagedRollbackPending(target, id)
}
