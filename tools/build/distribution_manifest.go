package main

import (
	packagefiles "sidravia/internal/release/files"
)

func portablePayload(entries []archiveEntry, owned packagefiles.Manifest) ([]archiveEntry, error) {
	var result []archiveEntry
	for _, entry := range entries {
		if entry.name != "sidraviaupdater.exe" && entry.name != packagefiles.Name {
			result = append(result, entry)
		}
	}
	portableOwned := owned
	portableOwned.Files = nil
	for _, file := range owned.Files {
		if file.Path != "sidraviaupdater.exe" {
			portableOwned.Files = append(portableOwned.Files, file)
		}
	}
	raw, err := packagefiles.Encode(portableOwned)
	if err != nil {
		return nil, err
	}
	return append(result, archiveEntry{packagefiles.Name, raw, 0644}, archiveEntry{"sidravia.portable", nil, 0644}), nil
}
