//go:build linux

package productlayout

import (
	"os"
	"path/filepath"
)

// XDG requires relative or empty environment values to be ignored. Keep core
// discovery and GUI preference resolution on the same absolute user roots.
func xdgUserDirectory(variable, fallback string) (string, error) {
	if value := os.Getenv(variable); filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}
func platformUserConfigDir() (string, error) { return xdgUserDirectory("XDG_CONFIG_HOME", ".config") }
func platformUserCacheDir() (string, error)  { return xdgUserDirectory("XDG_CACHE_HOME", ".cache") }
