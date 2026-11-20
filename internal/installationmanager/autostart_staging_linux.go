//go:build linux

package installationmanager

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"sidravia/internal/productlayout"
)

const rpmAutostartStage = ".sidravia-autostart-rpm-"
const portableAutostartStage = ".sidravia-autostart-app-"
const autostartStageMarker = ".sidravia-stage-v1"

// The desktop writer reserves a unique path for each publication.
// Its contents can be retired after a crash without reading the
// partially written entry. Any unexpected object is kept intact.
func recoverAutostartStaging(path string) error {
	layout, err := productlayout.Resolve()
	if err != nil {
		return err
	}
	prefix := portableAutostartStage
	if productlayout.RPMManaged(layout) {
		prefix = rpmAutostartStage
	}
	root, err := productlayout.LinuxUserConfigDir()
	if err != nil || !filepath.IsAbs(root) {
		return errors.Join(ErrServiceConflict, err)
	}
	parent := filepath.Join(filepath.Clean(root), "autostart")
	if !autostartStageName(filepath.Base(path), prefix) || filepath.Join(parent, filepath.Base(path)) != path {
		return ErrServiceConflict
	}
	return removeAutostartStage(path)
}

func autostartStageName(name, prefix string) bool {
	name, ok := strings.CutPrefix(name, prefix)
	desktop, nonce, found := strings.Cut(name, ".desktop-")
	if !ok || !found || len(nonce) != 32 {
		return false
	}
	_, err := hex.DecodeString(nonce)
	_, canonical := desktopEntryNamespace(desktop + ".desktop")
	return err == nil && canonical
}

func removeAutostartStage(path string) error {
	if err := plainDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&(unix.S_IFMT|07777) != unix.S_IFDIR|0700 {
		return ErrServiceConflict
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	original, err := dir.Stat(".")
	if err != nil {
		return err
	}
	if native, ok := original.Sys().(*syscall.Stat_t); !ok || native.Ino != stat.Ino || native.Dev != stat.Dev {
		return ErrServiceConflict
	}
	handle, err := dir.Open(".")
	if err != nil {
		return err
	}
	entries, err := handle.ReadDir(-1)
	if err := errors.Join(err, handle.Close()); err != nil {
		return err
	}
	var staged, marker bool
	// Check every entry before deleting anything. Only a marker created
	// atomically before the payload can identify a partial write as ours.
	for _, entry := range entries {
		if entry.Name() != "entry" && entry.Name() != autostartStageMarker {
			return ErrServiceConflict
		}
		info, err := dir.Lstat(entry.Name())
		if entry.Name() == "entry" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		fileStat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || fileStat.Uid != stat.Uid || fileStat.Nlink != 1 {
			return ErrServiceConflict
		}
		if entry.Name() == autostartStageMarker {
			if info.Size() != 0 {
				return ErrServiceConflict
			}
			marker = true
		} else {
			staged = true
		}
	}
	if staged {
		if !marker {
			return ErrServiceConflict
		}
		if err := dir.Remove("entry"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if marker {
		if err := dir.Remove(autostartStageMarker); err != nil {
			return err
		}
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(original, current) {
		return errors.Join(ErrServiceConflict, err)
	}
	return os.Remove(path)
}

func cleanupRPMAutostartStages(root string) error {
	parent := filepath.Join(root, "autostart")
	if err := plainDirectory(parent); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	var resultErr error
	for _, entry := range entries {
		if autostartStageName(entry.Name(), rpmAutostartStage) {
			resultErr = errors.Join(resultErr, removeAutostartStage(filepath.Join(parent, entry.Name())))
		}
	}
	return resultErr
}
