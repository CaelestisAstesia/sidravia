package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	packagefiles "sidravia/internal/release/files"
)

// Native GUI inputs may contain runtime libraries and application assets, but
// not arbitrary siblings left in an incremental compiler output directory.
func checkGUIRuntime(entries []archiveEntry, windows bool) error {
	declaredFonts := map[string]bool{}
	actualFiles := map[string]bool{}
	for _, entry := range entries {
		actualFiles[entry.name] = true
		if entry.name != "data/flutter_assets/FontManifest.json" {
			continue
		}
		var families []struct {
			Fonts []struct {
				Asset string `json:"asset"`
			} `json:"fonts"`
		}
		if err := json.Unmarshal(entry.data, &families); err != nil {
			return fmt.Errorf("invalid GUI font manifest: %w", err)
		}
		if families == nil {
			return fmt.Errorf("invalid GUI font manifest: expected a list")
		}
		for _, family := range families {
			if family.Fonts == nil {
				return fmt.Errorf("invalid GUI font manifest: expected fonts")
			}
			for _, font := range family.Fonts {
				declaredFonts["data/flutter_assets/"+font.Asset] = true
			}
		}
	}
	for font := range declaredFonts {
		if !actualFiles[font] {
			return fmt.Errorf("GUI payload lacks declared font: %s", font)
		}
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.name)
		base := path.Base(name)
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") {
				return fmt.Errorf("GUI payload contains hidden build content: %s", entry.name)
			}
		}
		switch path.Ext(name) {
		case ".pdb", ".lib", ".exp", ".ilk", ".obj", ".o", ".a", ".log", ".d", ".map", ".tmp":
			return fmt.Errorf("GUI payload contains a build artifact: %s", entry.name)
		case ".woff", ".woff2":
			return fmt.Errorf("native GUI payload contains a web font: %s", entry.name)
		case ".ttf", ".otf", ".ttc":
			if !declaredFonts[entry.name] {
				return fmt.Errorf("GUI payload contains an undeclared font: %s", entry.name)
			}
		}
		if base == "libdartjni.so" || base == "dartjni.dll" || base == "libdartjni.dll" || base == "libjvm.so" || base == "jvm.dll" {
			return fmt.Errorf("GUI payload contains an unsupported Java runtime: %s", entry.name)
		}
		if name == "data/flutter_assets/notices" || name == "data/flutter_assets/assetmanifest.bin.json" {
			return fmt.Errorf("native GUI payload contains a web-only asset: %s", entry.name)
		}
		allowed := strings.HasPrefix(name, "data/") || strings.HasPrefix(name, "licenses/")
		if windows {
			allowed = allowed || name == "sidravia.exe" || name == "native_assets.json" || (!strings.Contains(name, "/") && strings.HasSuffix(name, ".dll"))
		} else {
			allowed = allowed || name == "sidravia_gui" || name == "flutter-license" || name == "lib/native_assets.json" || (strings.HasPrefix(name, "lib/") && strings.HasSuffix(name, ".so"))
		}
		if !allowed {
			return fmt.Errorf("unexpected GUI runtime path: %s", entry.name)
		}
	}
	return nil
}

func readGUI(root string, existing []archiveEntry, arch string) ([]archiveEntry, error) {
	if root == "" {
		return nil, nil
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("GUI directory must be absolute")
	}
	reserved := map[string]bool{packagefiles.Name: true, "sidravia.portable": true, "sidraviactl.exe": true, "sidraviad.exe": true, "sidraviamgr.exe": true, "sidraviaupdater.exe": true}
	for _, entry := range existing {
		reserved[strings.ToLower(entry.name)] = true
	}
	var entries []archiveEntry
	err := filepath.WalkDir(root, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("GUI payload contains a link")
		}
		if info.IsDir() {
			return nil
		}
		metadata, err := info.Info()
		if err != nil {
			return err
		}
		if !metadata.Mode().IsRegular() {
			return fmt.Errorf("GUI payload contains a non-regular file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if reserved[strings.ToLower(name)] || strings.ContainsAny(name, "\"\r\n;{}") {
			return fmt.Errorf("GUI payload name conflicts with installation content: %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !matchesWindowsGUI(strings.ToLower(name), data, arch) {
			return fmt.Errorf("Windows GUI image does not match %s: %s", arch, name)
		}
		entries = append(entries, archiveEntry{name, data, metadata.Mode().Perm()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := checkGUIRuntime(entries, true); err != nil {
		return nil, err
	}
	found := map[string]bool{}
	for _, entry := range entries {
		found[entry.name] = true
	}
	for _, name := range []string{"Sidravia.exe", "flutter_windows.dll", "data/app.so", "data/icudtl.dat", "data/flutter_assets/AssetManifest.bin", "data/flutter_assets/FontManifest.json", "data/flutter_assets/NOTICES.Z"} {
		if !found[name] {
			return nil, fmt.Errorf("GUI payload lacks required Release file: %s", name)
		}
	}
	return entries, nil
}
