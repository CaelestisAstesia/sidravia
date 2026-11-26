//go:build linux || windows

package filetxn

import (
	"context"
	"os"
)

// Preflight checks the still-running source and the complete staged payload
// before asking the application to stop. Apply repeats the checks after exit.
func Preflight(ctx context.Context, target string, old Inventory, p *Payload) error {
	if p == nil || p.root == nil || !Canonical(target) {
		return ErrInvalid
	}
	if old.manifest.OS != p.inventory.manifest.OS || old.manifest.Arch != p.inventory.manifest.Arch || old.manifest.PayloadKind != p.inventory.manifest.PayloadKind {
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
	if err := availableSpace(root, 1<<20); err != nil {
		return err
	}
	if err := VerifyInventory(ctx, root, old); err != nil {
		return err
	}
	if err := VerifyGUI(root, old); err != nil {
		return err
	}
	if err := VerifyInventory(ctx, p.root, p.inventory); err != nil {
		return err
	}
	if err := VerifyGUI(p.root, p.inventory); err != nil {
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
