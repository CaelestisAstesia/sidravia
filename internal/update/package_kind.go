package update

import (
	"os"
	"path/filepath"

	"sidravia/internal/productlayout"
)

// Package identity follows the current layout, never a release fallback: a GUI
// installation must not silently lose its GUI by selecting a core-only asset.
func defaultPackageKind(goos, arch string, layout productlayout.Layout) string {
	if goos == "windows" && arch == "amd64" {
		if layout.Mode == productlayout.ModeInstalled {
			return "setup"
		}
		if info, err := os.Stat(filepath.Join(layout.ExecutableDirectory, "Sidravia.exe")); err == nil && info.Mode().IsRegular() {
			return "gui-portable"
		}
	}
	if goos == "linux" && arch == "amd64" && layout.Mode == productlayout.ModePortable {
		if info, err := os.Stat(filepath.Join(layout.ExecutableDirectory, "sidravia_gui")); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return "gui-portable"
		}
	}
	return "core-portable"
}
