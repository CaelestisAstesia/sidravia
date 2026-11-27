package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	packagefiles "sidravia/internal/release/files"
)

func installerManifest(root string, entries, gui []archiveEntry) string {
	guiNames := map[string]bool{}
	for _, entry := range gui {
		guiNames[entry.name] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var out strings.Builder
	out.WriteString("[Files]\n")
	fmt.Fprintf(&out, "Source: \"%s\"; Flags: dontcopy\n", filepath.Join(root, "sidraviamgr.exe"))
	for _, entry := range entries {
		component := "core"
		if guiNames[entry.name] {
			component = "gui"
		}
		dir := filepath.Dir(filepath.FromSlash(entry.name))
		dest := "{app}"
		if dir != "." {
			dest += "\\" + dir
		}
		fmt.Fprintf(&out, "Source: \"%s\"; DestDir: \"%s\"; Components: %s; Flags: ignoreversion\n", filepath.Join(root, filepath.FromSlash(entry.name)), dest, component)
	}
	if len(gui) > 0 {
		out.WriteString("\n[InstallDelete]\n")
		for _, entry := range gui {
			fmt.Fprintf(&out, "Type: files; Name: \"{app}\\%s\"; Check: RemoveGUIComponent\n", filepath.FromSlash(entry.name))
		}
	}
	return out.String()
}

func portablePayload(entries []archiveEntry, owned packagefiles.Manifest) ([]archiveEntry, error) {
	var result []archiveEntry
	for _, entry := range entries {
		if entry.name != "app_icon.ico" && entry.name != "sidraviaupdater.exe" && entry.name != packagefiles.Name {
			result = append(result, entry)
		}
	}
	portableOwned := owned
	portableOwned.Files = nil
	for _, file := range owned.Files {
		if file.Path != "app_icon.ico" && file.Path != "sidraviaupdater.exe" {
			portableOwned.Files = append(portableOwned.Files, file)
		}
	}
	raw, err := packagefiles.Encode(portableOwned)
	if err != nil {
		return nil, err
	}
	return append(result, archiveEntry{packagefiles.Name, raw, 0644}, archiveEntry{"sidravia.portable", nil, 0644}), nil
}
