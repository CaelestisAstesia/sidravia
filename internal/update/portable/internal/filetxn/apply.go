package filetxn

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	packagefiles "sidravia/internal/release/files"
	"sidravia/internal/update/transaction"
)

// Apply replaces only unchanged, authenticated product files. The lifecycle
// owner must first reach Quiescent; this function also refuses live target
// processes. It stops at Applied, leaving restart/intent recovery to that owner.
// Failure after entering Applying leaves a durable repair requirement, never an
// automatic downgrade or restart of partly replaced programs.
func Apply(ctx context.Context, store *transaction.Store, id, target string, old Inventory, p *Payload) error {
	if store == nil || p == nil || p.root == nil || len(old.data) == 0 || !Canonical(target) || filepath.Dir(p.directory) != filepath.Dir(target) {
		return ErrInvalid
	}
	if err := applicationPlatform(old); err != nil {
		return err
	}
	if err := applicationPlatform(p.inventory); err != nil {
		return err
	}
	guard, err := applyGuard()
	if err != nil {
		return err
	}
	defer guard()
	release, err := store.AcquireExecution(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	record, exists, err := store.Read(ctx)
	if err != nil {
		return err
	}
	next := p.inventory
	if !exists || !BoundExecution(record, target, old, p) {
		return ErrInvalid
	}
	parent, err := os.OpenRoot(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer parent.Close()
	root, err := parent.OpenRoot(filepath.Base(target))
	if err != nil {
		return err
	}
	defer root.Close()
	original, err := root.Stat(".")
	if err != nil {
		return err
	}
	staged, err := p.root.Stat(".")
	if err != nil {
		return err
	}
	if !sameDevice(original, staged) {
		return ErrChanged
	}
	parentIdentity, err := parent.Stat(".")
	if err != nil {
		return err
	}
	sameTarget := func() error {
		absoluteParent, e := os.Lstat(filepath.Dir(target))
		if e != nil {
			return e
		}
		if !Canonical(target) || !os.SameFile(parentIdentity, absoluteParent) {
			return ErrChanged
		}
		info, e := parent.Lstat(filepath.Base(target))
		if e != nil {
			return e
		}
		stageInfo, stageErr := parent.Lstat(filepath.Base(p.directory))
		if stageErr != nil {
			return stageErr
		}
		if !os.SameFile(stageInfo, staged) || !stageInfo.IsDir() || !os.SameFile(info, original) || !info.IsDir() {
			return ErrChanged
		}
		return nil
	}
	if err := VerifyInventory(ctx, root, old); err != nil {
		return err
	}
	if err := VerifyInventory(ctx, p.root, next); err != nil {
		return err
	}
	if err := VerifyGUI(root, old); err != nil {
		return err
	}
	if err := VerifyGUI(p.root, next); err != nil {
		return err
	}
	marker, err := openRegular(root, "sidravia.portable")
	if err != nil {
		return err
	}
	info, err := marker.Stat()
	err = errors.Join(err, marker.Close())
	if err != nil {
		return err
	}
	if info.Size() != 0 {
		return ErrChanged
	}
	before, after := index(old), index(next)
	// New product paths cannot adopt unknown user files, including case aliases,
	// links, non-directory parents and file/directory layout changes.
	for name := range after {
		if _, known := before[name]; known {
			continue
		}
		if err := absentProductPath(root, name); err != nil {
			return err
		}
	}
	if err := sameTarget(); err != nil {
		return err
	}
	if err := ensureQuiescent(target); err != nil {
		return err
	}
	if err := availableSpace(root, 1<<20); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Durable sealing is the cancellation commit point. After it, finite disk
	// work and phase publication continue together even if the caller cancels.
	work, sealInfo, err := sealApplication(ctx, root, id)
	if err != nil {
		return err
	}
	_, changed, err := store.Advance(work, id, transaction.Quiescent, transaction.Applying, "", time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return transaction.ErrConflict
	}
	fail := func(cause error) error {
		_, _, recordErr := store.Advance(work, id, transaction.Applying, transaction.RepairRequired, "apply_failed", time.Now())
		return errors.Join(cause, recordErr)
	}
	names := make([]string, 0, len(after))
	for name := range after {
		names = append(names, name)
	}
	sort.Strings(names)
	rename := func(name string) error {
		if err := sameTarget(); err != nil {
			return err
		}
		// Recheck the exact path immediately before changing it. os.Root prevents
		// escaping even if a path is concurrently changed; quiescence owns writers.
		if prior, ok := before[name]; ok {
			if err := verifyFile(work, root, prior); err != nil {
				return err
			}
		} else if name != packagefiles.Name {
			if err := absentProductPath(root, name); err != nil {
				return err
			}
		}
		if name != packagefiles.Name {
			if err := verifyFile(work, p.root, after[name]); err != nil {
				return err
			}
		}
		if err := makeProductParents(root, name); err != nil {
			return err
		}
		_, replace := before[name]
		replace = replace || name == packagefiles.Name
		if err := publish(parent, filepath.Join(filepath.Base(p.directory), filepath.FromSlash(name)), filepath.Join(filepath.Base(target), filepath.FromSlash(name)), replace); err != nil {
			return err
		}
		return syncParents(root, name)
	}
	for _, name := range names {
		if err := rename(name); err != nil {
			return fail(err)
		}
	}
	// Only stale authenticated files are removed. Never recurse into directories
	// or remove unknown files, configuration, logs, namespaces or the marker.
	for _, entry := range old.manifest.Files {
		if _, keep := after[entry.Path]; keep {
			continue
		}
		if err := sameTarget(); err != nil {
			return fail(err)
		}
		if err := verifyFile(work, root, entry); err != nil {
			return fail(err)
		}
		if err := root.Remove(entry.Path); err != nil {
			return fail(err)
		}
		if err := syncParents(root, entry.Path); err != nil {
			return fail(err)
		}
	}
	// Publish the new inventory last; a partial update is not a new baseline.
	if err := sameTarget(); err != nil {
		return fail(err)
	}
	oldIndex, err := openRegular(root, packagefiles.Name)
	if err != nil {
		return fail(err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(oldIndex, packagefiles.SizeLimit+1))
	err = errors.Join(readErr, oldIndex.Close())
	if string(raw) != string(old.data) {
		return fail(ErrChanged)
	}
	if err != nil {
		return fail(err)
	}
	if err := rename(packagefiles.Name); err != nil {
		return fail(err)
	}
	if err := VerifyInventory(work, root, next); err != nil {
		return fail(err)
	}
	if err := sameTarget(); err != nil {
		return fail(err)
	}
	_, changed, err = store.Advance(work, id, transaction.Applying, transaction.Applied, "", time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return transaction.ErrConflict
	}
	if holdStartupSeal() {
		return nil
	}
	// Only a durable Applied record plus a fully verified new tree permits
	// removal. A failure keeps at least the record or seal blocking admission.
	if err := sameTarget(); err != nil {
		return err
	}
	currentSeal, err := root.Lstat(transaction.StartupSeal)
	if err != nil {
		return err
	}
	if !os.SameFile(sealInfo, currentSeal) {
		return ErrChanged
	}
	if err := root.Remove(transaction.StartupSeal); err != nil {
		return err
	}
	return syncDirectory(root, ".")
}
