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

	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
	"sidravia/internal/release/qtinstaller"
)

// IFW's comparison differs from SemVer for arbitrary prerelease identifiers.
var qtCandidateVersion = regexp.MustCompile(`^[0-9]{1,18}(\.[0-9]{1,18}){2}(-((alpha|beta|rc)|dev\.qt)\.[0-9]{1,18})?$`)

func validQtCandidateVersion(version string) bool {
	return qtCandidateVersion.MatchString(version) && validateProductVersion(version) == nil
}

func buildQtDistribution(cfg config, stage, base string, template []byte, entries, gui []archiveEntry, log io.Writer) ([]string, error) {
	packages := filepath.Join(stage, "qt-packages")
	components := map[string]string{}
	for _, name := range []string{release.QtCoreComponent, release.QtGUIComponent} {
		components[name] = filepath.Join(packages, name)
		if err := os.MkdirAll(filepath.Join(components[name], "meta"), 0755); err != nil {
			return nil, err
		}
	}
	type operation struct {
		Name      string   `xml:"name,attr"`
		Arguments []string `xml:"Argument"`
	}
	type component struct {
		XMLName            xml.Name `xml:"Package"`
		DisplayName        string
		Description        string
		Version            string
		ReleaseDate        string
		Default            bool
		ForcedInstallation bool        `xml:",omitempty"`
		Dependencies       string      `xml:",omitempty"`
		Operations         []operation `xml:"Operations>Operation,omitempty"`
	}
	releaseDate := time.Now().UTC().Format(time.DateOnly)
	core := component{DisplayName: "Core", Description: "Sidravia command line and background service", Version: cfg.version, ReleaseDate: releaseDate, ForcedInstallation: true}
	desktop := component{DisplayName: "GUI", Description: "Sidravia desktop application", Version: cfg.version, ReleaseDate: releaseDate, Default: true, Dependencies: release.QtCoreComponent + "->=" + cfg.version}
	if err := writeQtXML(filepath.Join(components[release.QtCoreComponent], "meta", "package.xml"), core); err != nil {
		return nil, err
	}
	desktop.Operations = []operation{{
		Name:      "CreateShortcut",
		Arguments: []string{"@TargetDir@/Sidravia.exe", "@StartMenuDir@/Sidravia.lnk"},
	}}
	if err := writeQtXML(filepath.Join(components[release.QtGUIComponent], "meta", "package.xml"), desktop); err != nil {
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
		name := release.QtCoreComponent
		if guiPaths[entry.name] {
			name = release.QtGUIComponent
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
		StartMenuDir                 string
		DisableAuthorizationFallback bool
		PersistentLocalCache         bool
		RemoveTargetDir              bool
	}{
		Name: "Sidravia", Version: cfg.version, Title: "Sidravia Qt Validation", Publisher: "Sidravia",
		TargetDir: "@HomeDir@/AppData/Local/Programs/Sidravia Qt Validation", MaintenanceToolName: "sidravia-maintenance", StartMenuDir: "Sidravia Qt Validation",
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
	templateFile := filepath.Join(stage, "installerbase.exe")
	if err := os.WriteFile(templateFile, template, 0644); err != nil {
		return nil, err
	}
	setup := base + "-qt-setup.exe"
	setupFile := filepath.Join(stage, setup)
	if err := tool("binarycreator", "--offline-only", "-t", templateFile, "-c", configFile, "-p", packages, setupFile); err != nil {
		return nil, err
	}
	if err := verifyQtSetupPrefix(setupFile, template); err != nil {
		return nil, err
	}
	outputs := []string{setup}
	for _, profile := range []struct{ kind, include string }{
		{release.QtCoreRepository, release.QtCoreComponent},
		{release.QtGUIRepository, release.QtCoreComponent + "," + release.QtGUIComponent},
	} {
		repo := filepath.Join(stage, profile.kind)
		if err := tool("repogen", "-p", packages, "-i", profile.include, repo); err != nil {
			return nil, err
		}
		name := base + "-" + profile.kind + ".zip"
		if err := packageQtRepository(repo, filepath.Join(stage, name), cfg.target, release.QtRepositoryProfile(profile.kind), cfg.version, cfg.buildID, sha256Hex(template)); err != nil {
			return nil, err
		}
		outputs = append(outputs, name)
	}
	return outputs, nil
}

// IFW copies this exact executable prefix to the installed maintenance program.
// Reject a binarycreator that rewrites the pinned image during assembly.
func verifyQtSetupPrefix(path string, template []byte) error {
	if err := qtinstaller.VerifyPrefix(path, int64(len(template)), sha256Hex(template)); err != nil {
		return fmt.Errorf("verify Qt binarycreator maintenance image: %w", err)
	}
	return nil
}

func writeQtXML(path string, value any) error {
	data, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0644)
}

// A release signing step signs this exact inventory before publishing a repository.
func packageQtRepository(directory, archive, target, profile, version, buildID, maintenanceSHA256 string) error {
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
	data, err := json.Marshal(release.QtRepositoryIndex{
		SchemaVersion: 1, Product: "Sidravia", Target: target, Profile: profile, Version: version,
		BuildID: buildID, MaintenanceSHA256: maintenanceSHA256, Files: inventory,
	})
	if err != nil {
		return err
	}
	return writeZip(archive, append(entries, archiveEntry{"sidravia-ifw-inventory.json", data, 0644}))
}
