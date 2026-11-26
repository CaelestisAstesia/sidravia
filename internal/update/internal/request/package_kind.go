package request

import (
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
