// Package productlayout resolves the explicit installed or portable runtime
// layout shared by the Sidravia CLI and daemon. Both processes call Resolve to
// obtain the same absolute paths for configurations, institution
// profiles, runtime info and the daemon log. Resolution only stat-checks the
// portable marker; it never creates, deletes, opens or migrates any file or
// directory.
package productlayout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Mode names an explicit runtime layout.
type Mode string

const (
	// ModeInstalled is the default layout. Authentication configurations and
	// institution profiles live under the OS user config directory, while
	// runtime info and the daemon log live under the OS user cache directory.
	ModeInstalled Mode = "installed"
	// ModePortable is the layout enabled by a regular sidravia.portable marker
	// next to the executable. All product paths live under the executable
	// directory and never depend on OS user directories.
	ModePortable Mode = "portable"
)

// Layout holds the resolved runtime locations. Every path is absolute and clean
// on success.
type Layout struct {
	Mode                         Mode
	ExecutableDirectory          string
	ConfigurationsPath           string
	InstitutionProfilesDirectory string
	RuntimeInfoPath              string
	DaemonLogPath                string
}

// portableMarker is the fixed regular-file marker that enables portable mode.
const portableMarker = "sidravia.portable"

// Resolve determines the explicit installed or portable runtime layout from the
// current process executable and the portable marker. It only resolves and
// stat-checks; it never creates, deletes, opens or migrates any file or
// directory. A marker that is a directory, symlink or other non-regular file
// fails instead of falling back to either mode, and any IO error other than
// not-exist is preserved as the cause.
func Resolve() (Layout, error) {
	return resolve(defaultResolver())
}

// osResolver bundles the host calls the resolver depends on, so tests can
// inject deterministic functions without enlarging the public API.
type osResolver struct {
	executable    func() (string, error)
	userConfigDir func() (string, error)
	userCacheDir  func() (string, error)
	lstat         func(string) (os.FileInfo, error)
}

func defaultResolver() osResolver {
	return osResolver{
		executable:    os.Executable,
		userConfigDir: os.UserConfigDir,
		userCacheDir:  os.UserCacheDir,
		lstat:         os.Lstat,
	}
}

func resolve(r osResolver) (Layout, error) {
	exe, err := r.executable()
	if err != nil {
		return Layout{}, fmt.Errorf("productlayout: resolve executable: %w", err)
	}
	exeDir := filepath.Dir(filepath.Clean(exe))
	if !filepath.IsAbs(exeDir) {
		return Layout{}, fmt.Errorf("productlayout: executable directory is not absolute: %q", exeDir)
	}

	mode, err := resolveMode(r.lstat, exeDir)
	if err != nil {
		return Layout{}, err
	}

	layout := Layout{
		Mode:                mode,
		ExecutableDirectory: exeDir,
	}
	switch mode {
	case ModeInstalled:
		configRoot, err := userRoot(r.userConfigDir, "config")
		if err != nil {
			return Layout{}, err
		}
		cacheRoot, err := userRoot(r.userCacheDir, "cache")
		if err != nil {
			return Layout{}, err
		}
		layout.ConfigurationsPath = filepath.Join(configRoot, "configurations.json")
		layout.InstitutionProfilesDirectory = filepath.Join(configRoot, "institution-profiles")
		layout.RuntimeInfoPath = filepath.Join(cacheRoot, "runtime.json")
		layout.DaemonLogPath = filepath.Join(cacheRoot, "logs", "sidraviad.log")
	case ModePortable:
		layout.ConfigurationsPath = filepath.Join(exeDir, "config", "configurations.json")
		layout.InstitutionProfilesDirectory = filepath.Join(exeDir, "config", "institution-profiles")
		layout.RuntimeInfoPath = filepath.Join(exeDir, "runtime", "runtime.json")
		layout.DaemonLogPath = filepath.Join(exeDir, "logs", "sidraviad.log")
	}
	return layout, nil
}

// resolveMode selects the installed or portable mode by stat-checking the
// marker without following symbolic links. A directory, symlink or other
// non-regular marker fails instead of falling back to either mode; any IO
// error other than not-exist is preserved as the cause.
func resolveMode(lstat func(string) (os.FileInfo, error), exeDir string) (Mode, error) {
	markerPath := filepath.Join(exeDir, portableMarker)
	info, err := lstat(markerPath)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return "", errors.New("productlayout: portable marker is not a regular file")
		}
		return ModePortable, nil
	case errors.Is(err, os.ErrNotExist):
		return ModeInstalled, nil
	default:
		return "", fmt.Errorf("productlayout: check portable marker: %w", err)
	}
}

// userRoot returns the cleaned Sidravia root under one OS user directory. It
// preserves the resolver cause on failure and rejects a non-absolute result so
// every successful path stays absolute.
func userRoot(dirFunc func() (string, error), label string) (string, error) {
	dir, err := dirFunc()
	if err != nil {
		return "", fmt.Errorf("productlayout: resolve %s directory: %w", label, err)
	}
	root := filepath.Clean(filepath.Join(dir, "Sidravia"))
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("productlayout: %s root is not absolute: %q", label, root)
	}
	return root, nil
}
