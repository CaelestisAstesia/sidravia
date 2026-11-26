package layout

import (
	"os"
	"path/filepath"
)

// RPMManaged recognizes only an exact marker in an installed layout.
func RPMManaged(layout Layout) bool {
	return layout.Mode == ModeInstalled && rpmMarker(layout, "sidravia.rpm", "sidravia-rpm-v1\n")
}

// RPMCore reports the flavor recorded by the core package producer.
func RPMCore(layout Layout) bool {
	return rpmMarker(layout, "sidravia.rpm-core", "sidravia-rpm-core-v1\n")
}

func rpmMarker(layout Layout, name, want string) bool {
	path := filepath.Join(layout.ExecutableDirectory, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(want)) {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == want
}
