package filetxn

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func (p *Payload) makeDirectories(name string) error {
	for dir := filepath.Dir(filepath.FromSlash(name)); dir != "."; dir = filepath.Dir(dir) {
		if err := p.root.MkdirAll(dir, 0700); err != nil {
			return err
		}
		info, err := p.root.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrChanged
		}
		if _, exists := p.directories[dir]; !exists {
			p.directories[dir] = info
		}
	}
	return nil
}

// Close removes only inodes this payload created. Renamed staging roots,
// substituted leaves and unknown contents are retained and reported, never
// recursively deleted. A failed cleanup does not justify deleting its parent.
func (p *Payload) Close() error {
	if p == nil || p.root == nil {
		return nil
	}
	defer func() { p.root = nil }()
	original, err := p.root.Stat(".")
	current, pathErr := os.Lstat(p.directory)
	if err != nil || pathErr != nil || !os.SameFile(original, current) {
		return errors.Join(ErrChanged, err, pathErr, p.root.Close())
	}
	var cleanup error
	remove := func(name string, want os.FileInfo) {
		info, err := p.root.Lstat(name)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			cleanup = errors.Join(cleanup, err)
			return
		}
		if !os.SameFile(info, want) {
			cleanup = errors.Join(cleanup, ErrChanged)
			return
		}
		cleanup = errors.Join(cleanup, p.root.Remove(name))
	}
	for name, info := range p.owned {
		remove(name, info)
	}
	dirs := make([]string, 0, len(p.directories))
	for name := range p.directories {
		dirs = append(dirs, name)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		remove(dir, p.directories[dir])
	}
	cleanup = errors.Join(cleanup, p.root.Close())
	if cleanup != nil {
		return cleanup
	}
	current, err = os.Lstat(p.directory)
	if err != nil || !os.SameFile(original, current) {
		return errors.Join(ErrChanged, err)
	}
	return os.Remove(p.directory) // empty owned directory only
}
