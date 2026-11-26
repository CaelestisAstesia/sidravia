//go:build linux

package portable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/userservice"
	"sidravia/internal/installation/userservice/registration"
	"time"
	"sidravia/internal/installation/change"
	"sidravia/internal/process/linuxproc"
	"sidravia/internal/release"
	"sidravia/internal/update/transaction"
)

// ReconcileInterrupted releases a stranded, byte-verified admission after its
// exact coordinator has died. It NEVER retries a launch or authentication. A
// pending write/seal, unverified tree or live/unknown owner remains blocked.
func ReconcileInterrupted(ctx context.Context, store *transaction.Store, id, target string, installed Inventory) error {
	if store == nil || !canonical(target) {
		return ErrInvalid
	}
	record, exists, err := store.Read(ctx)
	if err != nil {
		return err
	}
	if !exists || record.ID != id || record.Source.ID != target {
		return transaction.ErrConflict
	}
	expected := record.Source
	next := transaction.Aborted
	code := "preparation_failed"
	switch record.Phase {
	case transaction.Created, transaction.Verified:
		if record.Phase == transaction.Verified {
			release, e := store.AcquireCoordinator(ctx, id)
			if e != nil {
				return e
			}
			defer release()
		}
	case transaction.Preparing, transaction.Quiescent, transaction.SourceStarting:
		if record.Recovery == nil {
			return transaction.ErrConflict
		}
	case transaction.Applied, transaction.Resuming, transaction.ServiceStarting:
		if record.Recovery == nil {
			return transaction.ErrConflict
		}
		expected = record.Target
		next = transaction.ResumeFailed
		code = "resume_failed"
	default:
		return transaction.ErrConflict
	}
	if installed.manifest.Version != expected.Version || installed.manifest.BuildID != expected.BuildID {
		return ErrInvalid
	}
	// Read() already authenticates this exact release proof and package. Keep
	// platform/payload boundaries for source verification, and exact asset identity
	// for target verification; a smaller core index cannot unlock a GUI tree.
	var manifest release.ReleaseManifest
	if err := json.Unmarshal(record.Manifest, &manifest); err != nil {
		return err
	}
	matched := false
	for _, asset := range manifest.Assets {
		if asset.Name == record.Package.Name && asset.Size == record.Package.Size && asset.SHA256 == record.Package.SHA256 {
			matched = installed.manifest.OS == asset.OS && installed.manifest.Arch == asset.Arch && installed.manifest.PayloadKind == asset.PayloadKind
			break
		}
	}
	if !matched {
		return ErrInvalid
	}
	if record.Phase == transaction.Resuming || record.Phase == transaction.Applied || record.Phase == transaction.ServiceStarting {
		c := installed.candidate
		if c.ID != record.Package.CandidateID || c.Name != record.Package.Name || c.Size != record.Package.Size || c.SHA256 != record.Package.SHA256 {
			return ErrInvalid
		}
	} else if record.Recovery != nil && digest(installed.data) != record.Recovery.SourceFilesSHA256 {
		return ErrInvalid
	}
	if record.Recovery != nil {
		alive, err := linuxproc.GenerationRunning(record.Recovery.CoordinatorPID, record.Recovery.CoordinatorStart)
		if err != nil {
			return err
		}
		if alive {
			return transaction.ErrConflict
		}
	}
	if _, err := os.Lstat(filepath.Join(target, transaction.StartupSeal)); !os.IsNotExist(err) {
		return errors.Join(ErrChanged, err)
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := verifyInventory(ctx, root, installed); err != nil {
		return err
	}
	if err := verifyGUI(root, installed); err != nil {
		return err
	}
	if record.Recovery != nil && record.Recovery.ManagedService &&
		(record.Phase == transaction.Preparing || record.Phase == transaction.Quiescent || record.Phase == transaction.SourceStarting) {
		return recoverManagedPreparation(ctx, store, record, installed)
	}
	_, changed, err := store.Advance(ctx, id, record.Phase, next, code, time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return transaction.ErrConflict
	}
	return nil
}

func recoverManagedPreparation(ctx context.Context, store *transaction.Store, record transaction.Record, installed Inventory) error {
	unlock, err := store.AcquireRollbackCoordinator(ctx, record.ID)
	if err != nil {
		return err
	}
	defer unlock()
	return userservice.WithServiceRegistration(ctx, func(lease *userservice.RegistrationLease) error {
		layout, found, err := registration.Lookup()
		if err != nil || !found || layout.Mode != productlayout.ModePortable || layout.ExecutableDirectory != record.Source.ID {
			return errors.Join(ErrChanged, err)
		}
		if err := cleanRollbackSnapshot(record, &installed); err != nil {
			return err
		}
		return resumeManagedPreparation(ctx, store, record, func(ctx context.Context, target change.Target) error {
			return startManagedService(ctx, lease, layout, target, true)
		})
	})

}
