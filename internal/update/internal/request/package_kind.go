package request

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"

	"sidravia/internal/release"

	productlayout "sidravia/internal/installation/layout"
)

// Package request follows the current layout, never a release fallback: a GUI
// installation must not silently lose its GUI by selecting a core-only asset.
func DefaultPackageKind(goos, arch string, layout productlayout.Layout) string {
	if goos == "windows" && release.DesktopArchitecture(arch) {
		if layout.Mode == productlayout.ModeInstalled {
			return "setup"
		}
		if info, err := os.Stat(filepath.Join(layout.ExecutableDirectory, "Sidravia.exe")); err == nil && info.Mode().IsRegular() {
			return "gui-portable"
		}
	}
	if goos == "linux" && release.DesktopArchitecture(arch) && layout.Mode == productlayout.ModePortable {
		if info, err := os.Stat(filepath.Join(layout.ExecutableDirectory, "sidravia_gui")); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return "gui-portable"
		}
	}
	return "core-portable"
}

// windowsInstalledKind uses IFW's component inventory when present. Qt
// binaries also carry a maintenance image identity, so loss of the entire
// IFW registration cannot turn a Qt installation into an Inno installation.
func windowsInstalledKind(layout productlayout.Layout) (string, error) {
	root, err := os.OpenRoot(layout.ExecutableDirectory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	maintenance, err := root.Lstat("sidravia-maintenance.exe")
	if errors.Is(err, os.ErrNotExist) {
		if QtMaintenanceDigest != "" {
			return "", release.ErrMetadata
		}
		for _, marker := range []string{"components.xml", "sidravia-maintenance.dat", "sidravia-maintenance.ini"} {
			if _, markerErr := root.Lstat(marker); !errors.Is(markerErr, os.ErrNotExist) {
				return "", release.ErrMetadata
			}
		}
		return "setup", nil
	}
	if err != nil || !maintenance.Mode().IsRegular() {
		return "", release.ErrMetadata
	}
	info, err := root.Lstat("components.xml")
	if err != nil || !info.Mode().IsRegular() || info.Size() > release.ReleaseManifestLimit {
		return "", release.ErrMetadata
	}
	file, err := root.Open("components.xml")
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, release.ReleaseManifestLimit+1))
	if err != nil || len(data) > release.ReleaseManifestLimit {
		return "", release.ErrMetadata
	}
	var inventory struct {
		XMLName    xml.Name `xml:"Packages"`
		Name       string   `xml:"ApplicationName"`
		Components []string `xml:"Package>Name"`
	}
	if xml.Unmarshal(data, &inventory) != nil || inventory.Name != "Sidravia" {
		return "", release.ErrMetadata
	}
	var core, gui bool
	for _, id := range inventory.Components {
		switch id {
		case release.QtCoreComponent:
			if core {
				return "", release.ErrMetadata
			}
			core = true
		case release.QtGUIComponent:
			if gui {
				return "", release.ErrMetadata
			}
			gui = true
		default:
			return "", release.ErrMetadata
		}
	}
	if !core {
		return "", release.ErrMetadata
	}
	if gui {
		return release.QtGUIRepository, nil
	}
	return release.QtCoreRepository, nil
}
