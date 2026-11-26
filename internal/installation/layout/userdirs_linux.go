//go:build linux

package layout

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// LinuxUserConfigDir resolves the XDG root shared by desktop integration and the installed layout.
func LinuxUserConfigDir() (string, error) { return xdgUserDirectory("XDG_CONFIG_HOME", ".config") }

func platformUserConfigDir() (string, error) { return LinuxUserConfigDir() }
func platformUserCacheDir() (string, error)  { return xdgUserDirectory("XDG_CACHE_HOME", ".cache") }

// Use NSS rather than os/user.Current, which falls back to the process HOME
// for some accounts in the release's pure-Go build.
func AccountHome(uid int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	id := strconv.Itoa(uid)
	cmd := exec.CommandContext(ctx, "/usr/bin/getent", "passwd", id)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), ":")
	if len(fields) != 7 || fields[2] != id || !filepath.IsAbs(fields[5]) ||
		filepath.Clean(fields[5]) != fields[5] || strings.ContainsAny(fields[5], "\x00\r\n") {
		return "", errors.New("invalid account home")
	}
	return fields[5], nil
}
