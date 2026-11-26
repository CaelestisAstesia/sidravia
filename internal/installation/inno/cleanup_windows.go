//go:build windows

package inno

import (
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"sidravia/internal/document/jsondoc"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/winproc"
)

// ClearUninstallRecords discards retired manual Setup records only for the
// installation and namespace the uninstaller is about to remove.
func ClearUninstallRecords(ctx context.Context, coordinator uint32) error {
	if coordinator == 0 {
		return change.ErrInvalid
	}
	peer, err := observeInstaller(coordinator)
	if err != nil {
		return err
	}
	defer peer.Close()
	if !strings.EqualFold(filepath.Base(peer.Path), "_unins.tmp") {
		return winproc.ErrPeer
	}
	layout, err := productlayout.Resolve()
	if err != nil {
		return err
	}
	if layout.Mode != productlayout.ModeInstalled {
		return change.ErrInvalid
	}
	root, err := productlayout.OperationRoot()
	if err != nil {
		return err
	}
	return clearRetiredRecords(ctx, root, layout)
}

func clearRetiredRecords(ctx context.Context, root string, layout productlayout.Layout) error {
	family := filepath.Join(root, operationFamily(layout.ExecutableDirectory, layout.Namespace))
	// Reject a reparse point in any parent before creating a scoped root. No
	// ancestor or sibling of this one installation's family is ever removed.
	for path := family; ; path = filepath.Dir(path) {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		attributes, err := windows.GetFileAttributes(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return change.ErrInvalid
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	scope, err := os.OpenRoot(family)
	if err != nil {
		return err
	}
	children, err := fs.ReadDir(scope.FS(), ".")
	removed := false
	if err == nil {
		for _, child := range children {
			if !child.IsDir() || !change.ValidID(child.Name()) {
				continue
			}
			if err = ctx.Err(); err != nil {
				break
			}
			var retired bool
			retired, err = retireRecord(scope, child.Name(), layout)
			if err != nil {
				break
			}
			removed = removed || retired
		}
	}
	if err = errors.Join(err, scope.Close()); err != nil {
		return err
	}
	if !removed {
		return nil
	}
	return removeEmptyRecordDirectory(family)
}

func retireRecord(scope *os.Root, op string, layout productlayout.Layout) (bool, error) {
	path := filepath.Join(op, "state.json")
	info, err := scope.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	file, err := scope.Open(path)
	if err != nil {
		return false, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return false, errors.Join(change.ErrInvalid, err, file.Close())
	}
	data, readErr := jsondoc.ReadLimited(file, 16<<10)
	if err = errors.Join(readErr, file.Close()); err != nil {
		return false, err
	}
	var state record
	if jsondoc.DecodeStrict(data, &state) != nil || !ownsManualRecord(state, op, layout) {
		return false, nil
	}
	if state.Stage != "stopped" && state.Stage != "aborted" {
		return false, change.ErrInvalid
	}
	for _, generation := range []processID{state.Coordinator, state.Daemon, state.GUI} {
		if generation.PID == 0 && generation.Created == 0 {
			continue
		}
		running, err := winproc.GenerationRunning(generation.PID, generation.Created)
		if err != nil || running {
			return false, errors.Join(change.ErrInvalid, err)
		}
	}
	current, err := scope.Lstat(path)
	if err != nil || !os.SameFile(current, info) {
		return false, errors.Join(change.ErrInvalid, err)
	}
	if err = scope.Remove(path); err != nil {
		return false, err
	}
	err = scope.Remove(op)
	if errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return true, err
}

func ownsManualRecord(state record, op string, layout productlayout.Layout) bool {
	if state.SchemaVersion != 1 || state.OperationID != op ||
		state.Source.Layout != string(productlayout.ModeInstalled) ||
		!strings.EqualFold(state.Source.ID, layout.ExecutableDirectory) || state.Namespace != layout.Namespace.String() ||
		state.Coordinator.PID == 0 || state.Coordinator.Created == 0 || state.Source.BuildID == "" || state.Source.Version == "" ||
		len(state.Hashes) < 2 {
		return false
	}
	for name, value := range state.Hashes {
		if name != "sidraviactl.exe" && name != "sidraviad.exe" && name != "Sidravia.exe" {
			return false
		}
		if hash, err := hex.DecodeString(value); err != nil || len(hash) != 32 {
			return false
		}
	}
	return state.Hashes["sidraviactl.exe"] != "" && state.Hashes["sidraviad.exe"] != ""
}

func removeEmptyRecordDirectory(path string) error {
	err := os.Remove(path)
	if errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
