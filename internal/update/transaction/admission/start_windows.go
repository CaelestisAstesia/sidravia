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

// Check verifies ordinary Windows startup or grants portable cold recovery. A
// separate Setup receipt is required if the blocked installation is installed.
func Check(ctx context.Context, target string, r Recovery) (needsReceipt bool, err error) {
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
		_, err := store.AcquireRecoveryStart(ctx, r.operation, r.token, r.version, r.build, r.namespace, r.configuration)
		if err != nil {
			return false, errors.Join(ErrRecoveryDenied, err)
		}
		return false, nil
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
