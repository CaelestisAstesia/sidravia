//go:build linux

package portable

import (
	"context"
	"debug/elf"
	"errors"
	"io"
	"os"
	"path/filepath"

	"sidravia/internal/update/packagefiles"
)

func verifyGUI(root *os.Root, i Inventory) error {
	if i.manifest.PayloadKind != "gui" {
		return nil
	}
	files := index(i)
	for _, name := range []string{"sidravia_gui", "gui-runtime/sidravia_gui", "gui-runtime/lib/libflutter_linux_gtk.so", "gui-runtime/lib/libapp.so", "gui-runtime/data/icudtl.dat"} {
		if _, ok := files[name]; !ok {
			return ErrUnsupported
		}
	}
	if files["sidravia_gui"].Mode != 0755 || files["gui-runtime/sidravia_gui"].Mode != 0755 {
		return ErrUnsupported
	}
	f, err := openRegular(root, "sidravia_gui")
	if err != nil {
		return err
	}
	defer f.Close()
	binary, err := elf.NewFile(f)
	if err != nil {
		return err
	}
	for _, program := range binary.Progs {
		if program.Type == elf.PT_INTERP || program.Type == elf.PT_DYNAMIC {
			return ErrUnsupported
		}
	}
	// Unknown runtime libraries/assets may be loaded by Flutter. Unlike unknown
	// files elsewhere in the portable tree, these cannot safely be left active.
	pending := []string{"gui-runtime"}
	visited := 0
	for len(pending) > 0 {
		directory := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		handle, err := root.Open(directory)
		if err != nil {
			return err
		}
		for {
			entries, readErr := handle.ReadDir(128)
			for _, entry := range entries {
				visited++
				if visited > packagefiles.MaximumFiles*4 {
					handle.Close()
					return ErrChanged
				}
				name := filepath.ToSlash(filepath.Join(directory, entry.Name()))
				info, err := root.Lstat(name)
				if err != nil {
					handle.Close()
					return err
				}
				if info.IsDir() {
					if err := checkParents(root, name+"/leaf"); err != nil {
						handle.Close()
						return err
					}
					pending = append(pending, name)
					continue
				}
				if !info.Mode().IsRegular() {
					handle.Close()
					return ErrChanged
				}
				if _, ok := files[name]; !ok {
					handle.Close()
					return ErrChanged
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				handle.Close()
				return readErr
			}
		}
		if err := handle.Close(); err != nil {
			return err
		}
	}
	return nil
}

// Preflight checks the still-running source and the complete staged payload
// before asking the application to stop. Apply repeats the checks after exit.
func Preflight(ctx context.Context, target string, old Inventory, p *Payload) error {
	if p == nil || p.root == nil || !canonical(target) {
		return ErrInvalid
	}
	if err := applicationPlatform(old); err != nil {
		return err
	}
	if err := applicationPlatform(p.inventory); err != nil {
		return err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := verifyInventory(ctx, root, old); err != nil {
		return err
	}
	if err := verifyGUI(root, old); err != nil {
		return err
	}
	if err := verifyInventory(ctx, p.root, p.inventory); err != nil {
		return err
	}
	if err := verifyGUI(p.root, p.inventory); err != nil {
		return err
	}
	before := index(old)
	for name := range index(p.inventory) {
		if _, ok := before[name]; !ok {
			if err := absentProductPath(root, name); err != nil {
				return err
			}
		}
	}
	return nil
}
