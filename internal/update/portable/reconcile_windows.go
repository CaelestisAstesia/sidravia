//go:build windows

package portable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/process/winproc"
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/transaction"
	"time"
)

// ReconcileInterrupted verifies complete signed bytes and dead exact owners.
// It never retries launch, intent dispatch or a partial file replacement.
func reconcileInterrupted(ctx context.Context, store *transaction.Store, id, target string, installed filetxn.Inventory) error {
	if store == nil || !filetxn.Canonical(target) {
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
		alive, e := winproc.GenerationRunning(owner.PID, owner.Created)
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
	if installed.Identity().Version != expected.Version || installed.Identity().BuildID != expected.BuildID || installed.Identity().OS != old.Identity().OS || installed.Identity().Arch != old.Identity().Arch || installed.Identity().PayloadKind != old.Identity().PayloadKind {
		return ErrInvalid
	}
	if targetBytes {
		if !installed.MatchesPackage(r.Package) {
			return ErrInvalid
		}
	} else if !installed.MatchesIndex(s.OldIndex) {
		return ErrInvalid
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = filetxn.VerifyInventory(ctx, root, installed); err != nil {
		return err
	}
	if err = filetxn.VerifyGUI(root, installed); err != nil {
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
		return filetxn.RemoveSeal(target, id)
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
