//go:build linux

package portable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/update/releases"
	"time"

	"sidravia/internal/update/linuxplatform"
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
	if !exists || record.ID != id || record.Source.ID != target || record.Recovery == nil {
		return operations.ErrConflict
	}
	expected := record.Source
	next := operations.Aborted
	code := "preparation_failed"
	switch record.Phase {
	case operations.Preparing, operations.Quiescent:
	case operations.Resuming:
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
	if record.Phase == operations.Resuming {
		c := installed.candidate
		if c.ID != record.Package.CandidateID || c.Name != record.Package.Name || c.Size != record.Package.Size || c.SHA256 != record.Package.SHA256 {
			return ErrInvalid
		}
	} else if digest(installed.data) != record.Recovery.SourceFilesSHA256 {
		return ErrInvalid
	}
	alive, err := linuxplatform.GenerationRunning(record.Recovery.CoordinatorPID, record.Recovery.CoordinatorStart)
	if err != nil {
		return err
	}
	if alive {
		return operations.ErrConflict
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
	_, changed, err := store.Advance(ctx, id, record.Phase, next, code, time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return operations.ErrConflict
	}
	return nil
}
