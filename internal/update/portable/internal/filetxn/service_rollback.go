//go:build linux || windows

package filetxn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	packagefiles "sidravia/internal/release/files"
	"sidravia/internal/update/transaction"
)

// Keep a verified copy of the old product before the first write. Its private
// sibling directory cannot be confused with credentials or the new staging tree.
func rollbackDirectory(target, id string) string {
	return filepath.Join(filepath.Dir(target), ".sidravia-service-rollback-"+id)
}

const rollbackNextIndex = ".next-index.json"
const rollbackDirectories = ".new-directories.json"

func stageServiceRollback(ctx context.Context, target string, old Inventory, directory string) (backup *Payload, resultErr error) {
	root, err := os.OpenRoot(target)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, err
	}
	snapshot, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errors.Join(err, os.Remove(directory))
	}
	staged := &Payload{directory: directory, root: snapshot, inventory: old,
		owned: make(map[string]os.FileInfo), directories: make(map[string]os.FileInfo)}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, staged.Close())
		}
	}()
	if err := staged.writeStage(ctx, packagefiles.Name, 0644, int64(len(old.data)), bytes.NewReader(old.data), digest(old.data)); err != nil {
		return nil, err
	}
	for _, entry := range old.manifest.Files {
		source, err := openRegular(root, entry.Path)
		if err != nil {
			return nil, err
		}
		err = errors.Join(staged.writeStage(ctx, entry.Path, modeFor(old, entry.Mode), entry.Size, source, entry.SHA256), source.Close())
		if err != nil {
			return nil, err
		}
	}
	return staged, errors.Join(VerifyInventory(ctx, snapshot, old), syncDirectory(snapshot, "."))
}

func persistRollbackInputs(ctx context.Context, backup *Payload, next Inventory, directories []string) error {
	data, err := json.Marshal(directories)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		data []byte
	}{
		{rollbackNextIndex, next.data}, {rollbackDirectories, data},
	} {
		if err := backup.writeStage(ctx, item.name, 0600, int64(len(item.data)), bytes.NewReader(item.data), digest(item.data)); err != nil {
			return err
		}
	}
	for dir := range backup.directories {
		if err := syncDirectory(backup.root, dir); err != nil {
			return err
		}
	}
	parent, err := os.OpenRoot(filepath.Dir(backup.directory))
	if err != nil {
		return err
	}
	defer parent.Close()
	return errors.Join(syncDirectory(backup.root, "."), syncDirectory(parent, "."))
}

func loadRollback(ctx context.Context, store *transaction.Store, record transaction.Record) (backup *Payload, next Inventory, dirs []string, resultErr error) {
	directory := rollbackDirectory(record.Source.ID, record.ID)
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, next, nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || !Canonical(directory) {
		return nil, next, nil, ErrChanged
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, next, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, root.Close())
		}
	}()
	read := func(name string) ([]byte, error) {
		f, err := openRegular(root, name)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, packagefiles.SizeLimit+1))
		return data, errors.Join(err, f.Close())
	}
	oldIndex, err := read(packagefiles.Name)
	if err != nil || digest(oldIndex) != record.Recovery.SourceFilesSHA256 {
		return nil, next, nil, errors.Join(ErrChanged, err)
	}
	old, err := packagefiles.Decode(oldIndex)
	if err != nil || old.Version != record.Source.Version || old.BuildID != record.Source.BuildID {
		return nil, next, nil, errors.Join(ErrChanged, err)
	}
	newIndex, err := read(rollbackNextIndex)
	if err != nil {
		return nil, next, nil, err
	}
	newManifest, err := store.AuthenticateTargetIndex(ctx, record.ID, newIndex)
	if err != nil || old.OS != newManifest.OS || old.Arch != newManifest.Arch || old.PayloadKind != newManifest.PayloadKind {
		return nil, next, nil, errors.Join(ErrChanged, err)
	}
	raw, err := read(rollbackDirectories)
	if err != nil || json.Unmarshal(raw, &dirs) != nil {
		return nil, next, nil, errors.Join(ErrChanged, err)
	}
	next = Inventory{manifest: newManifest, data: newIndex}
	oldInventory := Inventory{manifest: old, data: oldIndex}
	possibleDirs := make(map[string]bool)
	for _, entry := range next.manifest.Files {
		for parent := filepath.Dir(entry.Path); parent != "."; parent = filepath.Dir(parent) {
			possibleDirs[parent] = true
		}
	}
	for _, entry := range old.Files {
		for parent := filepath.Dir(entry.Path); parent != "."; parent = filepath.Dir(parent) {
			delete(possibleDirs, parent)
		}
	}
	for _, dir := range dirs {
		if !possibleDirs[dir] {
			return nil, next, nil, ErrChanged
		}
	}
	known := &Payload{directory: directory, root: root, inventory: oldInventory, owned: make(map[string]os.FileInfo), directories: make(map[string]os.FileInfo)}
	if err := VerifyInventory(ctx, root, oldInventory); err != nil {
		return nil, next, nil, err
	}
	paths := []string{packagefiles.Name, rollbackNextIndex, rollbackDirectories}
	for _, entry := range old.Files {
		paths = append(paths, entry.Path)
	}
	for _, name := range paths {
		info, err := root.Lstat(name)
		if err != nil {
			return nil, next, nil, err
		}
		known.owned[name] = info
		for dir := filepath.Dir(name); dir != "."; dir = filepath.Dir(dir) {
			info, err := root.Lstat(dir)
			if err != nil || !info.IsDir() {
				return nil, next, nil, errors.Join(ErrChanged, err)
			}
			known.directories[dir] = info
		}
	}
	return known, next, dirs, nil
}

// Snapshot directories absent before publication, so rollback can remove
// empty directories introduced by Apply without claiming existing user paths.
func newProductDirectories(target string, next Inventory) ([]string, error) {
	root, err := os.OpenRoot(target)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	absent := make(map[string]bool)
	for _, entry := range next.manifest.Files {
		for dir := filepath.Dir(entry.Path); dir != "."; dir = filepath.Dir(dir) {
			if _, err := root.Lstat(dir); errors.Is(err, os.ErrNotExist) {
				absent[dir] = true
			} else if err != nil {
				return nil, err
			}
		}
	}
	dirs := make([]string, 0, len(absent))
	for dir := range absent {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	return dirs, nil
}

// restoreServiceFiles only removes or replaces authenticated product bytes.
// If either package can no longer account for a path, keep the seal and backup
// so remediation can proceed without destroying the unknown file.
func (b *Rollback) Restore(ctx context.Context, store *transaction.Store, record transaction.Record) error {
	target, id := record.Source.ID, record.ID
	backup, next, dirs := b.backup, b.next, b.dirs
	old := backup.inventory
	if backup.directory != rollbackDirectory(target, id) {
		return ErrInvalid
	}
	release, err := store.AcquireExecution(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if err := ensureQuiescent(target); err != nil {
		return err
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
	if err := VerifyInventory(ctx, backup.root, old); err != nil {
		return err
	}
	seal, err := openRegular(root, transaction.StartupSeal)
	if errors.Is(err, os.ErrNotExist) {
		_, _, err = sealApplication(ctx, root, id)
	} else if err == nil {
		data, readErr := io.ReadAll(io.LimitReader(seal, 34))
		err = errors.Join(readErr, seal.Close())
		if err == nil && string(data) != id+"\n" {
			err = ErrChanged
		}
	}
	if err != nil {
		return err
	}
	oldFiles, nextFiles := index(old), index(next)
	// Treat the inventory like a product file but publish it last.
	catalog := func(i Inventory) packagefiles.File {
		return packagefiles.File{Path: packagefiles.Name, SHA256: digest(i.data), Size: int64(len(i.data)), Mode: 0644}
	}
	oldFiles[packagefiles.Name] = catalog(old)
	nextFiles[packagefiles.Name] = catalog(next)
	names := make([]string, 0, len(oldFiles)+len(nextFiles))
	for name := range oldFiles {
		if name != packagefiles.Name {
			names = append(names, name)
		}
	}
	for name := range nextFiles {
		if _, found := oldFiles[name]; !found {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	names = append(names, packagefiles.Name)
	for _, name := range names {
		oldEntry, hadOld := oldFiles[name]
		nextEntry, hadNext := nextFiles[name]
		_, err := root.Lstat(name)
		exists := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if exists && hadOld && verifyFile(ctx, root, oldEntry) == nil {
			continue
		}
		if exists && (!hadNext || verifyFile(ctx, root, nextEntry) != nil) {
			return ErrChanged
		}
		if hadOld {
			if err := makeProductParents(root, name); err != nil {
				return err
			}
			if err := publishRollbackFile(ctx, parent, root, backup, oldEntry, exists, id); err != nil {
				return err
			}
		} else if exists {
			if err := root.Remove(name); err != nil {
				return err
			}
		}
		if hadOld || exists {
			if err := syncParents(root, name); err != nil {
				return err
			}
		}
	}
	for _, dir := range dirs {
		// These directories had no prior contents. A concurrent new entry
		// prevents removal and belongs to its owner.
		info, err := root.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() {
			return errors.Join(ErrChanged, err)
		}
		if err := root.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			return err
		}
	}
	if err := errors.Join(VerifyInventory(ctx, root, old), VerifyGUI(root, old)); err != nil {
		return err
	}
	if err := root.Remove(transaction.StartupSeal); err != nil {
		return err
	}
	if err := syncDirectory(root, "."); err != nil {
		return err
	}
	_, changed, err := store.Advance(ctx, id, transaction.RollingBack, transaction.RolledBack, "update_rolled_back", time.Now())
	if err == nil && !changed {
		err = transaction.ErrConflict
	}
	return err
}

func publishRollbackFile(ctx context.Context, parent, target *os.Root, backup *Payload, entry packagefiles.File, replace bool, id string) error {
	// The scratch file is confined to the operation's private snapshot. The
	// snapshot itself is immutable, so an incomplete scratch copy is disposable.
	name := ".restore-" + id + "-" + digest([]byte(entry.Path))[:16]
	stageCopy := entry
	stageCopy.Path = name
	if existing, err := openRegular(backup.root, name); err == nil {
		if err := existing.Close(); err != nil {
			return err
		}
		if err := backup.root.Remove(name); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	source, err := openRegular(backup.root, entry.Path)
	if err != nil {
		return err
	}
	err = errors.Join(backup.writeStage(ctx, name, os.FileMode(entry.Mode), entry.Size, source, entry.SHA256), source.Close())
	if err != nil {
		return err
	}
	if err := verifyFile(ctx, backup.root, stageCopy); err != nil {
		return err
	}
	return publish(parent, filepath.Join(filepath.Base(backup.directory), name), filepath.Join(filepath.Base(target.Name()), entry.Path), replace)
}

// ManagedRollbackPending reports a snapshot that must be reconciled before
// another update can supersede the current operation.
func ManagedRollbackPending(target, id string) (bool, error) {
	_, err := os.Lstat(rollbackDirectory(target, id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Only an exact, operation-bound private snapshot is eligible for cleanup.
// While recovering preparation, the caller supplies the verified source
// inventory, so even a truncated snapshot index can be retired safely.
func CleanRollbackSnapshot(record transaction.Record, source *Inventory) error {
	directory := rollbackDirectory(record.Source.ID, record.ID)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !Canonical(directory) {
		return errors.Join(ErrChanged, err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if source == nil {
		f, err := openRegular(root, packagefiles.Name)
		if err != nil {
			// The index is the last leaf removed; an empty snapshot only
			// needs its final directory unlink retried.
			if errors.Is(err, os.ErrNotExist) {
				return os.Remove(directory)
			}
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, packagefiles.SizeLimit+1))
		if err := errors.Join(readErr, f.Close()); err != nil {
			return err
		}
		manifest, err := packagefiles.Decode(data)
		if err != nil || digest(data) != record.Recovery.SourceFilesSHA256 ||
			manifest.Version != record.Source.Version || manifest.BuildID != record.Source.BuildID {
			return errors.Join(ErrChanged, err)
		}
		source = &Inventory{manifest: manifest, data: data}
	}
	names := []string{rollbackNextIndex, rollbackDirectories}
	parents := make(map[string]bool)
	for _, entry := range source.manifest.Files {
		names = append(names, entry.Path, ".restore-"+record.ID+"-"+digest([]byte(entry.Path))[:16])
		for p := filepath.Dir(entry.Path); p != "."; p = filepath.Dir(p) {
			parents[p] = true
		}
	}
	names = append(names, ".restore-"+record.ID+"-"+digest([]byte(packagefiles.Name))[:16])
	removeFile := func(name string) error {
		f, err := openRegular(root, name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return root.Remove(name)
	}
	for _, name := range names {
		if err := removeFile(name); err != nil {
			return err
		}
	}
	dirs := make([]string, 0, len(parents))
	for parent := range parents {
		dirs = append(dirs, parent)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		info, err := root.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() {
			return errors.Join(ErrChanged, err)
		}
		if err := root.Remove(dir); err != nil {
			return err
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	files, readErr := dir.ReadDir(2)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, dir.Close()); err != nil || len(files) > 1 || len(files) == 1 && files[0].Name() != packagefiles.Name {
		return errors.Join(ErrChanged, err)
	}
	if err := removeFile(packagefiles.Name); err != nil {
		return err
	}
	current, err := os.Lstat(directory)
	if err != nil || !os.SameFile(info, current) {
		return errors.Join(ErrChanged, err)
	}
	return os.Remove(directory)
}

// Rollback retains the old files and the exact next inventory under a single
// handle. Closing the handle leaves the durable copy available to recovery.
type Rollback struct {
	backup *Payload
	next   Inventory
	dirs   []string
}

func (b *Rollback) Close() error { return b.backup.root.Close() }
func (b *Rollback) Retire(record transaction.Record) error {
	return errors.Join(b.Close(), CleanRollbackSnapshot(record, &b.backup.inventory))
}

func PrepareRollback(ctx context.Context, record transaction.Record, old Inventory, next *Payload) (*Rollback, error) {
	target := record.Source.ID
	dirs, err := newProductDirectories(target, next.inventory)
	if err != nil {
		return nil, err
	}
	backup, err := stageServiceRollback(ctx, target, old, rollbackDirectory(target, record.ID))
	if err != nil {
		return nil, err
	}
	if err := persistRollbackInputs(ctx, backup, next.inventory, dirs); err != nil {
		return nil, errors.Join(err, backup.root.Close(), CleanRollbackSnapshot(record, &old))
	}
	return &Rollback{backup: backup, next: next.inventory, dirs: dirs}, nil
}

func OpenRollback(ctx context.Context, store *transaction.Store, record transaction.Record) (*Rollback, error) {
	backup, next, dirs, err := loadRollback(ctx, store, record)
	if err != nil {
		return nil, err
	}
	return &Rollback{backup: backup, next: next, dirs: dirs}, nil
}
