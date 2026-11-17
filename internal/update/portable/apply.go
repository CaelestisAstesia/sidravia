package portable

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sidravia/internal/update/operations"
	"sidravia/internal/update/packagefiles"
)

// Apply replaces only unchanged, authenticated product files. The lifecycle
// owner must first reach Quiescent; this function also refuses live target
// processes. It stops at Applied, leaving restart/intent recovery to that owner.
// Failure after entering Applying leaves a durable repair requirement, never an
// automatic downgrade or restart of partly replaced programs.
func Apply(ctx context.Context, store *operations.Store, id, target string, old Inventory, p *Payload) error {
	if store == nil || p == nil || p.root == nil || len(old.data) == 0 || !canonical(target) || filepath.Dir(p.directory) != filepath.Dir(target) {
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
	if !exists || !boundExecution(record, target, old, p) {
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
		if !canonical(target) || !os.SameFile(parentIdentity, absoluteParent) {
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
	if err := verifyInventory(ctx, root, old); err != nil {
		return err
	}
	if err := verifyInventory(ctx, p.root, next); err != nil {
		return err
	}
	if err := verifyGUI(root, old); err != nil {
		return err
	}
	if err := verifyGUI(p.root, next); err != nil {
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
	_, changed, err := store.Advance(work, id, operations.Quiescent, operations.Applying, "", time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return operations.ErrConflict
	}
	fail := func(cause error) error {
		_, _, recordErr := store.Advance(work, id, operations.Applying, operations.RepairRequired, "apply_failed", time.Now())
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
	if err := verifyInventory(work, root, next); err != nil {
		return fail(err)
	}
	if err := sameTarget(); err != nil {
		return fail(err)
	}
	_, changed, err = store.Advance(work, id, operations.Applying, operations.Applied, "", time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return operations.ErrConflict
	}
	if holdStartupSeal() {
		return nil
	}
	// Only a durable Applied record plus a fully verified new tree permits
	// removal. A failure keeps at least the record or seal blocking admission.
	if err := sameTarget(); err != nil {
		return err
	}
	currentSeal, err := root.Lstat(operations.StartupSeal)
	if err != nil {
		return err
	}
	if !os.SameFile(sealInfo, currentSeal) {
		return ErrChanged
	}
	if err := root.Remove(operations.StartupSeal); err != nil {
		return err
	}
	return syncDirectory(root, ".")
}

func absentProductPath(root *os.Root, name string) error {

	// Enumerate each directory to reject case aliases on case-sensitive hosts
	// too; packages must keep the same portable path identity on Windows.
	current := "."
	for _, part := range strings.Split(name, "/") {
		f, err := root.Open(current)
		if err != nil {
			return err
		}
		entries, readErr := f.Readdirnames(-1)
		closeErr := f.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		found := ""
		for _, entry := range entries {
			if pathKey(entry) == pathKey(part) {
				if entry != part {
					return ErrChanged
				}
				found = entry
				break
			}
		}
		current = filepath.Join(current, part)
		if found == "" {
			return nil
		}
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrChanged
		}
	}
	return ErrChanged // an existing directory cannot become a product file
}
func makeProductParents(root *os.Root, name string) error {
	dir := filepath.Dir(filepath.FromSlash(name))
	if dir == "." {
		return nil
	}
	// Parent paths were preflighted; refuse links at the point of use as well.
	if err := root.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return checkParents(root, name)
}

func syncParents(root *os.Root, name string) error {
	for dir := filepath.Dir(filepath.FromSlash(name)); ; dir = filepath.Dir(dir) {
		if err := syncDirectory(root, dir); err != nil {
			return err
		}
		if dir == "." {
			return nil
		}
	}
}

func sealApplication(ctx context.Context, root *os.Root, id string) (context.Context, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	seal, err := root.OpenFile(operations.StartupSeal, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	_, writeErr := io.WriteString(seal, id+"\n")
	if writeErr == nil {
		writeErr = seal.Sync()
	}
	writeErr = errors.Join(writeErr, seal.Close())
	if writeErr != nil {
		return nil, nil, writeErr
	}
	info, err := root.Lstat(operations.StartupSeal)
	if err != nil {
		return nil, nil, err
	}
	if err := syncDirectory(root, "."); err != nil {
		return nil, nil, err
	}
	return context.WithoutCancel(ctx), info, nil
}

func boundExecution(record operations.Record, target string, old Inventory, p *Payload) bool {
	if p == nil {
		return false
	}
	next := p.inventory
	return !(record.Source.ID != target || record.Source.Layout != "portable" || record.Source.Version != old.manifest.Version || record.Source.BuildID != old.manifest.BuildID || record.Target.Version != next.manifest.Version || record.Target.BuildID != next.manifest.BuildID || record.Package.CandidateID != next.candidate.ID || record.Package.Name != next.candidate.Name || record.Package.Size != next.candidate.Size || record.Package.SHA256 != next.candidate.SHA256 || old.manifest.OS != next.manifest.OS || old.manifest.Arch != next.manifest.Arch || old.manifest.PayloadKind != next.manifest.PayloadKind)
}
