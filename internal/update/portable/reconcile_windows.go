//go:build windows

package portable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/update/transaction"
	"sidravia/internal/update/windowsplatform"
	"time"
)

// ReconcileInterrupted verifies complete signed bytes and dead exact owners.
// It never retries launch, intent dispatch or a partial file replacement.
func ReconcileInterrupted(ctx context.Context, store *transaction.Store, id, target string, installed Inventory) error {
	if store == nil || !canonical(target) {
		return ErrInvalid
	}
	r, exists, err := store.Read(ctx)
	if err != nil {
		return err
	}
	if !exists || r.ID != id || r.Source.ID != target || r.Source.Layout != "portable" {
		return transaction.ErrConflict
	}
	if r.Phase == transaction.Applying || r.Phase == transaction.RepairRequired {
		return transaction.ErrConflict
	}
	s, dir, err := readWindowsSession(ctx, target, id)
	if err != nil {
		return err
	}
	if s.Source != r.Source {
		return ErrInvalid
	}
	owners := []runnerID{s.Updater, s.Caller}
	var ready runnerID
	if err = readPrivate(ctx, filepath.Join(dir, "ready.json"), &ready); err == nil {
		owners = append(owners, ready)
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, owner := range owners {
		if owner.PID == 0 {
			continue
		}
		alive, e := windowsplatform.GenerationRunning(owner.PID, owner.Created)
		if e != nil {
			return e
		}
		if alive {
			return transaction.ErrConflict
		}
	}
	expected := r.Source
	next := transaction.Aborted
	code := "preparation_failed"
	targetBytes := false
	terminal := false
	switch r.Phase {
	case transaction.Created, transaction.Verified, transaction.Preparing, transaction.Quiescent:
	case transaction.Applied, transaction.Resuming:
		expected = r.Target
		targetBytes = true
		next = transaction.ResumeFailed
		code = "resume_failed"
	case transaction.Completed, transaction.ResumeFailed:
		expected = r.Target
		targetBytes = true
		terminal = true
	case transaction.Aborted:
		terminal = true
	default:
		return transaction.ErrConflict
	}
	old, err := sourceInventory(s)
	if err != nil {
		return err
	}
	if installed.manifest.Version != expected.Version || installed.manifest.BuildID != expected.BuildID || installed.manifest.OS != old.manifest.OS || installed.manifest.Arch != old.manifest.Arch || installed.manifest.PayloadKind != old.manifest.PayloadKind {
		return ErrInvalid
	}
	if targetBytes {
		c := installed.candidate
		if c.ID != r.Package.CandidateID || c.Name != r.Package.Name || c.Size != r.Package.Size || c.SHA256 != r.Package.SHA256 {
			return ErrInvalid
		}
	} else if string(installed.data) != string(s.OldIndex) {
		return ErrInvalid
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = verifyInventory(ctx, root, installed); err != nil {
		return err
	}
	if err = verifyGUI(root, installed); err != nil {
		return err
	}
	sealed, err := ownPortableSeal(root, id)
	if err != nil {
		return err
	}
	if sealed && !targetBytes && r.Phase != transaction.Quiescent && r.Phase != transaction.Aborted {
		return ErrChanged
	}
	if !terminal {
		if _, changed, e := store.Advance(ctx, id, r.Phase, next, code, time.Now()); e != nil || !changed {
			return errors.Join(transaction.ErrConflict, e)
		}
	}
	if sealed {
		return finishApplication(target, id)
	}
	return nil
}
func ownPortableSeal(root *os.Root, id string) (bool, error) {
	_, err := root.Lstat(transaction.StartupSeal)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = transaction.VerifyPortableSeal(root.Name(), id); err != nil {
		return false, err
	}
	return true, nil
}
