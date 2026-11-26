package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sidravia/internal/connection/auth/supported"
	profilecatalog "sidravia/internal/connection/profile/catalog"
	"sidravia/internal/document/jsondoc"
)

// loadOfficialProfileEntries snapshots and validates the complete official
// Profile source directory before returning the exact bytes to package.
func loadOfficialProfileEntries(repoRoot string) ([]archiveEntry, error) {
	sourceDir := filepath.Join(repoRoot, "internal", "connection", "profile", "data")
	info, err := os.Lstat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("stat source directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source path is not a directory")
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("read source directory: %w", err)
	}
	sources := make([]profilecatalog.Source, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(sourceDir, name)
		fileInfo, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		if !fileInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", name)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", name, err)
		}
		openedInfo, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("stat opened %s: %w", name, err)
		}
		if !openedInfo.Mode().IsRegular() {
			file.Close()
			return nil, fmt.Errorf("%s is not a regular file", name)
		}
		data, err := jsondoc.ReadLimited(file, profilecatalog.FileSizeLimit)
		closeErr := file.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", name, closeErr)
		}
		sources = append(sources, profilecatalog.Source{Name: name, Data: data})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("source directory contains no lowercase .json Profiles")
	}

	if _, err := profilecatalog.LoadCaptured(context.Background(), sources, supported.ValidateInstitution); err != nil {
		return nil, fmt.Errorf("validate complete Profile set: %w", err)
	}

	profiles := make([]archiveEntry, 0, len(sources))
	for _, source := range sources {
		id := strings.TrimSuffix(source.Name, ".json")
		profiles = append(profiles, archiveEntry{
			name: "institution-profiles/" + id + ".json",
			data: source.Data,
			mode: 0o644,
		})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].name < profiles[j].name })
	return profiles, nil
}
