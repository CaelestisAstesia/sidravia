package productlayout

import (
	"os"
	"path/filepath"
)

// RPMManaged recognizes only an exact marker in an installed layout.
func RPMManaged(layout Layout) bool {
	if layout.Mode != ModeInstalled {
		return false
	}
	path := filepath.Join(layout.ExecutableDirectory, "sidravia.rpm")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("sidravia-rpm-v1\n")) {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == "sidravia-rpm-v1\n"
}
