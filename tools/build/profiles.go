package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/persistence/jsonfile"
)

// loadOfficialProfileEntries snapshots and validates the complete official
// Profile source directory before returning the exact bytes to package.
func loadOfficialProfileEntries(repoRoot string) ([]zipEntry, error) {
	sourceDir := filepath.Join(repoRoot, "internal", "daemon", "configuration", "profiles")
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
	type profileSource struct {
		name string
		data []byte
	}
	sources := make([]profileSource, 0, len(entries))
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
		data, err := jsonfile.ReadLimited(file, jsonfile.ProfileFileSizeLimit)
		closeErr := file.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", name, closeErr)
		}
		sources = append(sources, profileSource{name: name, data: data})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("source directory contains no lowercase .json Profiles")
	}

	snapshotDir, err := os.MkdirTemp("", "sidravia-official-profiles-")
	if err != nil {
		return nil, fmt.Errorf("create validation snapshot: %w", err)
	}
	defer os.RemoveAll(snapshotDir)
	for _, source := range sources {
		if err := os.WriteFile(filepath.Join(snapshotDir, source.name), source.data, 0o600); err != nil {
			return nil, fmt.Errorf("write validation snapshot %s: %w", source.name, err)
		}
	}

	registry, err := protocol.NewAuthenticationProtocolRegistry(d520.NewFactory())
	if err != nil {
		return nil, fmt.Errorf("create protocol registry: %w", err)
	}
	if _, err := configuration.LoadProfileCatalogFromDirectory(context.Background(), snapshotDir, registry); err != nil {
		return nil, fmt.Errorf("validate complete Profile set: %w", err)
	}

	profiles := make([]zipEntry, 0, len(sources))
	for _, source := range sources {
		id := strings.TrimSuffix(source.name, ".json")
		profiles = append(profiles, zipEntry{
			name: "institution-profiles/" + id + ".json",
			data: source.data,
			mode: 0o644,
		})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].name < profiles[j].name })
	return profiles, nil
}
