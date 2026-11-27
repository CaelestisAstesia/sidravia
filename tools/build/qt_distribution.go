package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	packagefiles "sidravia/internal/release/files"
)

const (
	qtCoreComponent = "org.sidravia.core"
	qtGUIComponent  = "org.sidravia.gui"
)

// IFW's comparison differs from SemVer for arbitrary prerelease identifiers.
var qtCandidateVersion = regexp.MustCompile(`^[0-9]{1,18}(\.[0-9]{1,18}){2}(-((alpha|beta|rc)|dev\.qt)\.[0-9]{1,18})?$`)

func validQtCandidateVersion(version string) bool {
	return qtCandidateVersion.MatchString(version) && validateProductVersion(version) == nil
}

func buildQtDistribution(cfg config, stage, base string, entries, gui []archiveEntry, log io.Writer) ([]string, error) {
	packages := filepath.Join(stage, "qt-packages")
	components := map[string]string{}
	for _, name := range []string{qtCoreComponent, qtGUIComponent} {
		components[name] = filepath.Join(packages, name)
		if err := os.MkdirAll(filepath.Join(components[name], "meta"), 0755); err != nil {
			return nil, err
		}
	}
	type component struct {
		XMLName      xml.Name `xml:"Package"`
		DisplayName  string
		Description  string
		Version      string
		ReleaseDate  string
		Default      bool
		Dependencies string `xml:",omitempty"`
	}
	releaseDate := time.Now().UTC().Format(time.DateOnly)
	core := component{DisplayName: "Core", Description: "Sidravia command line and background service", Version: cfg.version, ReleaseDate: releaseDate}
	desktop := component{DisplayName: "GUI", Description: "Sidravia desktop application", Version: cfg.version, ReleaseDate: releaseDate, Default: true, Dependencies: qtCoreComponent + "->=" + cfg.version}
	if err := writeQtXML(filepath.Join(components[qtCoreComponent], "meta", "package.xml"), core); err != nil {
		return nil, err
	}
	if err := writeQtXML(filepath.Join(components[qtGUIComponent], "meta", "package.xml"), desktop); err != nil {
		return nil, err
	}
	guiPaths := make(map[string]bool, len(gui))
	for _, entry := range gui {
		guiPaths[entry.name] = true
	}
	for _, entry := range entries {
		if entry.name == packagefiles.Name {
			continue // Qt owns the installed component inventory.
		}
		name := qtCoreComponent
		if guiPaths[entry.name] {
			name = qtGUIComponent
		}
		destination := filepath.Join(components[name], "data", filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(destination, entry.data, entry.mode); err != nil {
			return nil, err
		}
	}
	installerConfig := struct {
		XMLName                      xml.Name `xml:"Installer"`
		Name, Version, Title         string
		Publisher, TargetDir         string
		MaintenanceToolName          string
		DisableAuthorizationFallback bool
		PersistentLocalCache         bool
		RemoveTargetDir              bool
	}{
		Name: "Sidravia", Version: cfg.version, Title: "Sidravia Qt Validation", Publisher: "Sidravia",
		TargetDir: "@HomeDir@/AppData/Local/Programs/Sidravia Qt Validation", MaintenanceToolName: "sidravia-maintenance",
		DisableAuthorizationFallback: true,
	}
	configFile := filepath.Join(stage, "qt-installer.xml")
	if err := writeQtXML(configFile, installerConfig); err != nil {
		return nil, err
	}
	tool := func(name string, args ...string) error {
		command := exec.Command(filepath.Join(cfg.ifw, name+".exe"), args...)
		command.Stdout, command.Stderr = log, log
		if err := command.Run(); err != nil {
			return fmt.Errorf("run Qt %s: %w", name, err)
		}
		return nil
	}
	setup := base + "-qt-setup.exe"
	if err := tool("binarycreator", "--offline-only", "-t", filepath.Join(cfg.ifw, "installerbase.exe"), "-c", configFile, "-p", packages, filepath.Join(stage, setup)); err != nil {
		return nil, err
	}
	outputs := []string{setup}
	for _, profile := range []struct{ name, include string }{
		{"core", qtCoreComponent},
		{"gui", qtCoreComponent + "," + qtGUIComponent},
	} {
		repo := filepath.Join(stage, "qt-repository-"+profile.name)
		if err := tool("repogen", "-p", packages, "-i", profile.include, repo); err != nil {
			return nil, err
		}
		name := base + "-qt-" + profile.name + "-repository.zip"
		if err := packageQtRepository(repo, filepath.Join(stage, name)); err != nil {
			return nil, err
		}
		outputs = append(outputs, name)
	}
	return outputs, nil
}

func writeQtXML(path string, value any) error {
	data, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0644)
}

// A release signing step signs this exact inventory before publishing a repository.
func packageQtRepository(directory, archive string) error {
	var entries []archiveEntry
	inventory := map[string]string{}
	err := filepath.WalkDir(directory, func(file string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		if !item.Type().IsRegular() {
			return fmt.Errorf("invalid Qt repository entry: %s", file)
		}
		relative, err := filepath.Rel(directory, file)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		inventory[name] = sha256Hex(data)
		entries = append(entries, archiveEntry{name, data, 0644})
		return nil
	})
	if err != nil {
		return err
	}
	if _, ok := inventory["Updates.xml"]; !ok {
		return fmt.Errorf("Qt repository has no Updates.xml")
	}
	data, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	return writeZip(archive, append(entries, archiveEntry{"sidravia-ifw-inventory.json", data, 0644}))
}
