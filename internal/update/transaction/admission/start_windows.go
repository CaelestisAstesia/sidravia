package admission

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"sidravia/internal/installation/change"
	"sidravia/internal/update/transaction"
)

var ErrRecoveryDenied = errors.New("installation startup recovery denied")

// Check identifies blocked Setup startup. An old portable transaction
// cannot authorize startup of a potentially incomplete directory.
func Check(ctx context.Context, target string) (needsReceipt bool, err error) {
	_, sealErr := os.Lstat(filepath.Join(target, transaction.StartupSeal))
	sealed := !os.IsNotExist(sealErr)
	store, err := open(target)
	if err != nil {
		return false, err
	}
	record, exists, err := store.Read(ctx)
	if err != nil {
		return false, err
	}
	if !sealed && (!exists || !record.RequiresStartupBlock()) {
		return false, nil
	}
	if exists && record.Source.Layout == "portable" {
		return false, ErrRecoveryDenied
	}
	return true, nil
}

var ErrConflict = transaction.ErrConflict

// InstalledSource verifies the transaction part of Setup recovery. The receipt
// still has to bind the same source and authenticate the live finalizer ancestry.
func InstalledSource(ctx context.Context, target string, r Recovery) (operation string, source change.Target, err error) {
	store, err := open(target)
	if err != nil {
		return "", source, err
	}
	record, exists, err := store.Read(ctx)
	if err != nil {
		return "", source, err
	}
	rec := record.Recovery
	if !exists || record.ID != r.operation || record.Source.Layout != "installed" || record.Phase != transaction.Resuming ||
		rec == nil || rec.Nonce != r.token || record.Target.Version != r.version || record.Target.BuildID != r.build ||
		rec.Namespace != r.namespace || rec.Intent.ConfigurationID != r.configuration || !time.Now().Before(rec.ExpiresAt) {
		return "", source, transaction.ErrConflict
	}
	return record.ID, record.Source, nil
}
