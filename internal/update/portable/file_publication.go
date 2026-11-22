package portable

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/update/operations"
)

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
