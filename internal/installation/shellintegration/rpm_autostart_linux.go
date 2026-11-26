//go:build linux

package shellintegration

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/internal/integrationfile"
	productlayout "sidravia/internal/installation/layout"
)

const desktopEntryName = "io.github.CaelestisAstesia.Sidravia"

// Use the NSS home for the index so RPM can find custom XDG roots when their
// user has logged out. The index contains only directory names.
func lockPackageAutostart(create bool, manager string) (string, string, []string, func(), error) {
	home, err := productlayout.AccountHome(os.Geteuid())
	if err != nil || os.Geteuid() == 0 || !filepath.IsAbs(home) {
		return "", "", nil, nil, errors.Join(errRPMDesktopUnavailable, err)
	}
	root := filepath.Join(home, ".config")
	dir := filepath.Join(root, "Sidravia")
	index := filepath.Join(dir, "."+manager+"-autostart-roots.json")
	noop := func() {}
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", "", nil, nil, err
		}
	}
	unlock, err := lockAutostartDirectory(dir)
	if errors.Is(err, os.ErrNotExist) && !create {
		return root, "", nil, noop, nil
	}
	if err != nil {
		return "", "", nil, nil, err
	}
	if err := integrationfile.ClearScratch(index + ".sidravia-tmp"); err != nil {
		unlock()
		return "", "", nil, nil, err
	}
	data, err := readAutostartFile(index)
	if errors.Is(err, os.ErrNotExist) {
		return root, index, nil, unlock, nil
	}
	var roots []string
	if err != nil || json.Unmarshal(data, &roots) != nil || len(roots) > 128 {
		unlock()
		return "", "", nil, nil, errors.Join(errRPMDesktopConflict, err)
	}
	for _, root := range roots {
		if !validAutostartRoot(root) {
			unlock()
			return "", "", nil, nil, errRPMDesktopConflict
		}
	}
	return root, index, roots, unlock, nil
}

// Flutter locks this same directory inode while inspecting or changing an
// autostart entry. Both locks are nonblocking because the Flutter writer
// acquires the autostart directory before updating the package's root index.
func lockAutostartDirectory(dir string) (func(), error) {
	if err := plainDirectory(dir); err != nil {
		return nil, err
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return func() { _ = unix.Close(fd) }, nil
}

func validAutostartRoot(root string) bool {
	return filepath.IsAbs(root) && filepath.Clean(root) == root && len(root) < 4096 && !strings.ContainsRune(root, 0)
}

func plainDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && resolved != path {
		err = errRPMDesktopConflict
	}
	return err
}

func readAutostartFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0022 != 0 || stat.Size > 65536 {
		return nil, errRPMDesktopConflict
	}
	return io.ReadAll(io.LimitReader(f, 65537))
}

func TrackPackageAutostart(root string, enable bool, manager string) error {
	layout, err := productlayout.Resolve()
	owner, _ := productlayout.SystemPackage(layout)
	if err != nil || owner == "" || owner != manager || layout.ExecutableDirectory != productlayout.PackageDirectory(manager) || !validAutostartRoot(root) {
		return errors.Join(errRPMDesktopUnavailable, err)
	}
	defaultRoot, index, roots, unlock, err := lockPackageAutostart(enable, manager)
	if err != nil {
		return err
	}
	defer unlock()
	if root == defaultRoot || index == "" {
		return nil
	}
	if enable {
		if slices.Contains(roots, root) {
			return nil
		}
		roots = append(roots, root)
	} else {
		owned, err := packageAutostartFiles(root, manager)
		if err != nil || len(owned) > 0 {
			return err
		}
		entries, err := os.ReadDir(filepath.Join(root, "autostart"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() && autostartStageName(entry.Name(), packageAutostartStage(manager)) {
				return nil
			}
		}
		roots = slices.DeleteFunc(roots, func(item string) bool { return item == root })
	}
	if len(roots) == 0 {
		return integrationfile.Save(index, nil)
	}
	data, err := json.Marshal(roots)
	if err != nil || len(roots) > 128 || len(data) > 65536 {
		return errors.Join(errRPMDesktopConflict, err)
	}
	if err := integrationfile.Save(index, data); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(index))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func desktopEntryNamespace(name string) (string, bool) {
	stem, desktop := strings.CutSuffix(name, ".desktop")
	if !desktop {
		return "", false
	}
	if stem == desktopEntryName {
		return "", true
	}
	suffix, namespaced := strings.CutPrefix(stem, desktopEntryName+"-")
	if !namespaced || suffix == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != suffix {
		return "", false
	}
	ns, err := productlayout.NewNamespace(string(decoded))
	return ns.String(), err == nil && !ns.IsProduction()
}

func packageAutostartFiles(root, manager string) ([]string, error) {
	dir := filepath.Join(root, "autostart")
	if err := plainDirectory(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var owned []string
	var resultErr error
	for _, entry := range entries {
		namespace, desktop := desktopEntryNamespace(entry.Name())
		if !desktop || !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := readAutostartFile(path)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errRPMDesktopConflict) || errors.Is(err, unix.ELOOP) {
			continue
		}
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		if ownsPackageDesktopEntry(string(data), namespace, productlayout.PackageDirectory(manager)) {
			owned = append(owned, path)
		}
	}
	return owned, resultErr
}

func ownsPackageDesktopEntry(content, namespace, directory string) bool {
	for _, icon := range []string{"gui-runtime/data", "data"} {
		header := "[Desktop Entry]\nType=Application\nName=Sidravia\nComment=Sidravia session autostart\nIcon=" +
			directory + "/" + icon + "/icons/sidravia.png\nTerminal=false\n"
		env := "\"SIDRAVIA_NAMESPACE=" + namespace + "\""
		for _, mode := range []struct{ program, args string }{
			{"sidravia_gui", ""}, {"sidravia_gui", " --start-hidden"},
			{"sidraviactl", " daemon start --log-level info"},
		} {
			entry := "Exec=\"/usr/bin/env\" --chdir \"" + directory + "\" " + env +
				" \"./" + mode.program + "\"" + mode.args + "\nX-Sidravia-Owner=linux-desktop-v2\n"
			if content == header+entry || content == header+"TryExec="+directory+"/"+mode.program+"\n"+entry {
				return true
			}
		}
		if content == header+"Exec=\"/usr/bin/env\" "+env+" \""+directory+"/sidravia_gui\" --start-hidden\nX-Sidravia-Owner=linux-gui-v1\n" {
			return true
		}
	}
	return false
}

func CleanupErasedPackage(registered productlayout.Layout, manager string) error {
	directory := productlayout.PackageDirectory(manager)
	if directory == "" {
		return errRPMDesktopConflict
	}
	root, index, roots, unlock, lockErr := lockPackageAutostart(false, manager)
	if lockErr != nil {
		home, err := productlayout.AccountHome(os.Geteuid())
		if err != nil || !filepath.IsAbs(home) || os.Geteuid() == 0 {
			return errors.Join(lockErr, err)
		}
		// A damaged index cannot authorize additional roots, but neither
		// should it prevent cleanup in independently established locations.
		root = filepath.Join(home, ".config")
	} else {
		defer unlock()
	}
	recorded := slices.Clone(roots)
	roots = append(roots, root)
	if registered.Mode == productlayout.ModeInstalled && registered.ExecutableDirectory == directory {
		config := filepath.Dir(filepath.Dir(registered.ConfigurationsPath))
		if validAutostartRoot(config) {
			roots = append(roots, config)
		}
	}
	slices.Sort(roots)
	var remaining []string
	resultErr := lockErr
	for _, root := range slices.Compact(roots) {
		rootErr := cleanupPackageAutostartRoot(root, manager)
		if rootErr != nil && slices.Contains(recorded, root) {
			remaining = append(remaining, root)
		}
		resultErr = errors.Join(resultErr, rootErr)
	}
	if index != "" && lockErr == nil {
		var data []byte
		if len(remaining) > 0 {
			data, _ = json.Marshal(remaining) // A slice of strings is always encodable.
		}
		resultErr = errors.Join(resultErr, integrationfile.Save(index, data))
	}
	return resultErr
}

// Cleanup and the Flutter writer share the same lease for the entire scan and
// removal. A busy indexed root remains recorded for the next package callback.
func cleanupPackageAutostartRoot(root, manager string) error {
	unlock, err := lockAutostartDirectory(filepath.Join(root, "autostart"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	paths, scanErr := packageAutostartFiles(root, manager)
	resultErr := errors.Join(scanErr, cleanupPackageAutostartStages(root, manager))
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}
	return resultErr
}
