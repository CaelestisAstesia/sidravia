//go:build linux

package installationmanager

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
	"sidravia/internal/productlayout"
)

const desktopEntryName = "io.github.CaelestisAstesia.Sidravia"
const rpmAppDirectory = "/usr/lib64/sidravia"

// Use the NSS home for the index so RPM can find custom XDG roots when their
// user has logged out. The index contains only directory names.
func lockRPMAutostart(create bool) (string, string, []string, func(), error) {
	home, err := productlayout.AccountHome(os.Geteuid())
	if err != nil || os.Geteuid() == 0 || !filepath.IsAbs(home) {
		return "", "", nil, nil, errors.Join(ErrServiceUnavailable, err)
	}
	root := filepath.Join(home, ".config")
	dir := filepath.Join(root, "Sidravia")
	index := filepath.Join(dir, ".rpm-autostart-roots.json")
	noop := func() {}
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", "", nil, nil, err
		}
	}
	if err := plainDirectory(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) && !create {
			return root, "", nil, noop, nil
		}
		return "", "", nil, nil, err
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", "", nil, nil, err
	}
	unlock := func() { _ = unix.Close(fd) }
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unlock()
		return "", "", nil, nil, err
	}
	if err := clearRegistrationScratch(index + ".sidravia-tmp"); err != nil {
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
		return "", "", nil, nil, errors.Join(ErrServiceConflict, err)
	}
	for _, root := range roots {
		if !validAutostartRoot(root) {
			unlock()
			return "", "", nil, nil, ErrServiceConflict
		}
	}
	return root, index, roots, unlock, nil
}

func validAutostartRoot(root string) bool {
	return filepath.IsAbs(root) && filepath.Clean(root) == root && len(root) < 4096 && !strings.ContainsRune(root, 0)
}

func plainDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && resolved != path {
		err = ErrServiceConflict
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
		return nil, ErrServiceConflict
	}
	return io.ReadAll(io.LimitReader(f, 65537))
}

func trackRPMAutostart(root string, enable bool) error {
	layout, err := productlayout.Resolve()
	if err != nil || layout.ExecutableDirectory != rpmAppDirectory || !productlayout.RPMManaged(layout) || !validAutostartRoot(root) {
		return errors.Join(ErrServiceUnavailable, err)
	}
	defaultRoot, index, roots, unlock, err := lockRPMAutostart(enable)
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
		owned, err := rpmAutostartFiles(root)
		if err != nil || len(owned) > 0 {
			return err
		}
		entries, err := os.ReadDir(filepath.Join(root, "autostart"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() && autostartStageName(entry.Name(), rpmAutostartStage) {
				return nil
			}
		}
		roots = slices.DeleteFunc(roots, func(item string) bool { return item == root })
	}
	if len(roots) == 0 {
		return saveUnit(index, nil)
	}
	data, err := json.Marshal(roots)
	if err != nil || len(roots) > 128 || len(data) > 65536 {
		return errors.Join(ErrServiceConflict, err)
	}
	if err := saveUnit(index, data); err != nil {
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

func rpmAutostartFiles(root string) ([]string, error) {
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
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrServiceConflict) || errors.Is(err, unix.ELOOP) {
			continue
		}
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		if ownsRPMDesktopEntry(string(data), namespace) {
			owned = append(owned, path)
		}
	}
	return owned, resultErr
}

func ownsRPMDesktopEntry(content, namespace string) bool {
	for _, icon := range []string{"gui-runtime/data", "data"} {
		header := "[Desktop Entry]\nType=Application\nName=Sidravia\nComment=Sidravia session autostart\nIcon=" +
			rpmAppDirectory + "/" + icon + "/icons/sidravia.png\nTerminal=false\n"
		env := "\"SIDRAVIA_NAMESPACE=" + namespace + "\""
		for _, mode := range []struct{ program, args string }{
			{"sidravia_gui", "--start-hidden"}, {"sidraviactl", "daemon start --log-level info"},
		} {
			entry := "Exec=\"/usr/bin/env\" --chdir \"" + rpmAppDirectory + "\" " + env +
				" \"./" + mode.program + "\" " + mode.args + "\nX-Sidravia-Owner=linux-desktop-v2\n"
			if content == header+entry || content == header+"TryExec="+rpmAppDirectory+"/"+mode.program+"\n"+entry {
				return true
			}
		}
		if content == header+"Exec=\"/usr/bin/env\" "+env+" \""+rpmAppDirectory+"/sidravia_gui\" --start-hidden\nX-Sidravia-Owner=linux-gui-v1\n" {
			return true
		}
	}
	return false
}

func cleanupRPMAutostart(registered productlayout.Layout) error {
	root, index, roots, unlock, lockErr := lockRPMAutostart(false)
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
	if registered.Mode == productlayout.ModeInstalled && registered.ExecutableDirectory == rpmAppDirectory {
		config := filepath.Dir(filepath.Dir(registered.ConfigurationsPath))
		if validAutostartRoot(config) {
			roots = append(roots, config)
		}
	}
	slices.Sort(roots)
	var remaining []string
	resultErr := lockErr
	for _, root := range slices.Compact(roots) {
		paths, scanErr := rpmAutostartFiles(root)
		rootErr := errors.Join(scanErr, cleanupRPMAutostartStages(root))
		for _, path := range paths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				rootErr = errors.Join(rootErr, err)
			}
		}
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
		resultErr = errors.Join(resultErr, saveUnit(index, data))
	}
	return resultErr
}
