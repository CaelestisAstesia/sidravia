package layout

import (
	"os"
	"path/filepath"
)

// RPMExecutableDirectory is the fixed installation directory shared by Sidravia RPM variants.
const RPMExecutableDirectory = "/usr/lib64/sidravia"

// RPMManaged recognizes only an exact marker in an installed layout.
func RPMManaged(layout Layout) bool {
	return layout.Mode == ModeInstalled && packageMarker(layout, "sidravia.rpm", "sidravia-rpm-v1\n")
}

// RPMCore reports the flavor recorded by the core package producer.
func RPMCore(layout Layout) bool {
	return packageMarker(layout, "sidravia.rpm-core", "sidravia-rpm-core-v1\n")
}

const DEBExecutableDirectory = "/usr/lib/sidravia"

// SystemPackage identifies the operating system package that owns an installed payload.
func SystemPackage(layout Layout) (manager string, core bool) {
	if RPMManaged(layout) {
		return "rpm", RPMCore(layout)
	}
	if layout.Mode == ModeInstalled && packageMarker(layout, "sidravia.deb", "sidravia-deb-v1\n") {
		return "deb", packageMarker(layout, "sidravia.deb-core", "sidravia-deb-core-v1\n")
	}
	return "", false
}

func SystemManaged(layout Layout) bool {
	manager, _ := SystemPackage(layout)
	return manager != ""
}

// PackageDirectory only returns fixed system installation roots.
func PackageDirectory(manager string) string {
	switch manager {
	case "rpm":
		return RPMExecutableDirectory
	case "deb":
		return DEBExecutableDirectory
	}
	return ""
}

func packageMarker(layout Layout, name, want string) bool {
	path := filepath.Join(layout.ExecutableDirectory, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(want)) {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == want
}
