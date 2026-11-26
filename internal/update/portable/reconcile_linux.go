//go:build linux

package portable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/installationmanager"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/releases"
	"time"

	"sidravia/internal/process/linuxproc"
	"sidravia/internal/update"
	"sidravia/internal/update/operations"
)

// ReconcileInterrupted releases a stranded, byte-verified admission after its
// exact coordinator has died. It NEVER retries a launch or authentication. A
// pending write/seal, unverified tree or live/unknown owner remains blocked.
func ReconcileInterrupted(ctx context.Context, store *operations.Store, id, target string, installed Inventory) error {
	if store == nil || !canonical(target) {
		return ErrInvalid
	}
	record, exists, err := store.Read(ctx)
	if err != nil {
		return err
	}
	if !exists || record.ID != id || record.Source.ID != target {
		return operations.ErrConflict
	}
	expected := record.Source
	next := operations.Aborted
	code := "preparation_failed"
	switch record.Phase {
	case operations.Created, operations.Verified:
		if record.Phase == operations.Verified {
			release, e := store.AcquireCoordinator(ctx, id)
			if e != nil {
				return e
			}
			defer release()
		}
	case operations.Preparing, operations.Quiescent, operations.SourceStarting:
		if record.Recovery == nil {
			return operations.ErrConflict
		}
	case operations.Applied, operations.Resuming, operations.ServiceStarting:
		if record.Recovery == nil {
			return operations.ErrConflict
		}
		expected = record.Target
		next = operations.ResumeFailed
		code = "resume_failed"
	default:
		return operations.ErrConflict
	}
	if installed.manifest.Version != expected.Version || installed.manifest.BuildID != expected.BuildID {
		return ErrInvalid
	}
	// Read() already authenticates this exact release proof and package. Keep
	// platform/payload boundaries for source verification, and exact asset identity
	// for target verification; a smaller core index cannot unlock a GUI tree.
	var manifest releases.ReleaseManifest
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
	if record.Phase == operations.Resuming || record.Phase == operations.Applied || record.Phase == operations.ServiceStarting {
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
			return operations.ErrConflict
		}
	}
	if _, err := os.Lstat(filepath.Join(target, operations.StartupSeal)); !os.IsNotExist(err) {
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
		(record.Phase == operations.Preparing || record.Phase == operations.Quiescent || record.Phase == operations.SourceStarting) {
		return recoverManagedPreparation(ctx, store, record, installed)
	}
	_, changed, err := store.Advance(ctx, id, record.Phase, next, code, time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return operations.ErrConflict
	}
	return nil
}

func recoverManagedPreparation(ctx context.Context, store *operations.Store, record operations.Record, installed Inventory) error {
	unlock, err := store.AcquireRollbackCoordinator(ctx, record.ID)
	if err != nil {
		return err
	}
	defer unlock()
	return installationmanager.WithServiceRegistration(ctx, func(lease *installationmanager.RegistrationLease) error {
		layout, found, err := installationmanager.RegisteredService()
		if err != nil || !found || layout.Mode != productlayout.ModePortable || layout.ExecutableDirectory != record.Source.ID {
			return errors.Join(ErrChanged, err)
		}
		if err := cleanRollbackSnapshot(record, &installed); err != nil {
			return err
		}
		return resumeManagedPreparation(ctx, store, record, func(ctx context.Context, target update.Target) error {
			return startManagedService(ctx, lease, layout, target, true)
		})
	})

}
