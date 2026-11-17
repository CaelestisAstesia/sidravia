package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Native GUI inputs may contain runtime libraries and application assets, but
// not arbitrary siblings left in an incremental compiler output directory.
func checkGUIRuntime(entries []zipEntry, windows bool) error {
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
