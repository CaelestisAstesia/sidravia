package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/update/packagefiles"
)

// Only a fresh release GUI input is accepted. Core tools, public profiles and
// portable state belong to the distribution producer, never this input.
func readLinuxGUI(root string, existing []zipEntry) ([]zipEntry, error) {
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
	var entries []zipEntry
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
		if name == "sidravia_gui" || strings.HasSuffix(name, ".so") {
			if len(data) < 20 || !bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) || data[4] != 2 || data[5] != 1 || binary.LittleEndian.Uint16(data[18:20]) != 62 {
				return fmt.Errorf("Linux GUI is not ELF AMD64: %s", name)
			}
		}
		mode := os.FileMode(0644)
		if name == "sidravia_gui" {
			mode = 0755
		}
		entries = append(entries, zipEntry{"gui-runtime/" + name, data, mode})
		found[name] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	runtimeEntries := make([]zipEntry, len(entries))
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
