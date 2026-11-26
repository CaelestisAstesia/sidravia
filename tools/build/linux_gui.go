package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	packagefiles "sidravia/internal/release/files"
)

// Only a fresh release GUI input is accepted. Core tools, public profiles and
// portable state belong to the distribution producer, never this input.
func readLinuxGUI(root string, existing []archiveEntry, arch string) ([]archiveEntry, error) {
	if root == "" {
		return nil, nil
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("GUI directory must be absolute")
	}
	reserved := map[string]bool{packagefiles.Name: true}
	for _, e := range existing {
		reserved[strings.ToLower(e.name)] = true
	}
	var entries []archiveEntry
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Linux GUI payload contains a link")
		}
		if item.IsDir() {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Linux GUI payload contains a non-regular file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if reserved[strings.ToLower(name)] || !(name == "sidravia_gui" || name == "FLUTTER-LICENSE" || strings.HasPrefix(name, "lib/") || strings.HasPrefix(name, "data/")) || strings.ContainsAny(name, "\r\n\x00") {
			return fmt.Errorf("unexpected Linux GUI payload: %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if (name == "sidravia_gui" || strings.HasSuffix(strings.ToLower(name), ".so")) && !matchesELF(data, arch) {
			return fmt.Errorf("Linux GUI is not ELF %s: %s", arch, name)
		}
		mode := os.FileMode(0644)
		if name == "sidravia_gui" {
			mode = 0755
		}
		entries = append(entries, archiveEntry{"gui-runtime/" + name, data, mode})
		found[name] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	runtimeEntries := make([]archiveEntry, len(entries))
	for i, entry := range entries {
		runtimeEntries[i] = entry
		runtimeEntries[i].name = strings.TrimPrefix(entry.name, "gui-runtime/")
	}
	if err := checkGUIRuntime(runtimeEntries, false); err != nil {
		return nil, err
	}
	for _, name := range []string{"sidravia_gui", "lib/libflutter_linux_gtk.so", "lib/libapp.so", "data/icudtl.dat", "data/flutter_assets/NOTICES.Z", "data/flutter_assets/version.json", "data/icons/sidravia.png", "FLUTTER-LICENSE"} {
		if !found[name] {
			return nil, fmt.Errorf("Linux GUI payload lacks %s", name)
		}
	}
	return entries, nil
}
