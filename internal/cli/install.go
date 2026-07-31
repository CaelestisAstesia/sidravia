package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/productlayout"
)

// installTaskName is the exact reserved Windows Task Scheduler task name shared
// by install and uninstall. Decisions about the task are made by schtasks exit
// codes only; its definition is never parsed from localized output.
const installTaskName = "SidraviaDaemon"

// pathValueName is the per-user environment PATH value name under
// HKCU\Environment.
const pathValueName = "PATH"

// installLogDefault is the log level applied when install runs without an
// explicit --log-level.
const installLogDefault = "info"

// Registry value kinds for the per-user PATH value. They match the numeric
// values of golang.org/x/sys/windows/registry; the cross-platform logic does
// not import the Windows-only registry package.
const (
	pathKindSZ       uint32 = 1
	pathKindExpandSZ uint32 = 2
)

// installSystem is the platform seam for the Windows-only install and uninstall
// operations. Non-Windows entry points never construct it.
type installSystem interface {
	// readUserPath returns the current per-user PATH value and its registry
	// value kind. A missing value returns an empty value with kind 0.
	readUserPath() (value string, kind uint32, err error)
	// writeUserPath writes the per-user PATH value with the given kind.
	writeUserPath(value string, kind uint32) error
	// broadcastEnvironmentChange notifies running processes that the user
	// environment changed. Callers treat it as best-effort.
	broadcastEnvironmentChange() error
	// taskExists reports whether the named Task Scheduler task exists.
	taskExists(name string) (bool, error)
	// createTask creates or replaces the named task with the exact action.
	createTask(name string, action string) error
	// deleteTask deletes the named task.
	deleteTask(name string) error
}

// normalizePathEntry trims trailing path separators for comparison. Windows
// accepts both separators; the install directory is already absolute and clean.
func normalizePathEntry(entry string) string {
	return strings.TrimRight(entry, `/\`)
}

// pathWithEntry returns value with entry appended exactly once when it is not
// already present. Comparison is case-insensitive and ignores a trailing path
// separator; the appended entry carries no trailing separator. Existing
// entries, their order and separators are preserved.
func pathWithEntry(value, entry string) string {
	norm := normalizePathEntry(entry)
	if value == "" {
		return norm
	}
	for _, part := range strings.Split(value, ";") {
		if strings.EqualFold(normalizePathEntry(part), norm) {
			return value
		}
	}
	if strings.HasSuffix(value, ";") {
		return value + norm
	}
	return value + ";" + norm
}

// pathWithoutEntry removes the first exact case-insensitive match of entry from
// value and reports whether the value changed. Remaining entries keep their
// order and dangling separators are trimmed.
func pathWithoutEntry(value, entry string) (string, bool) {
	norm := normalizePathEntry(entry)
	removed := false
	kept := make([]string, 0, len(strings.Split(value, ";")))
	for _, part := range strings.Split(value, ";") {
		if !removed && part != "" && strings.EqualFold(normalizePathEntry(part), norm) {
			removed = true
			continue
		}
		kept = append(kept, part)
	}
	if !removed {
		return value, false
	}
	return strings.Trim(strings.Join(kept, ";"), ";"), true
}

// pathValueKind returns the registry value kind used to write a PATH value. An
// existing SZ or EXPAND_SZ kind is preserved; a missing or unknown kind uses
// EXPAND_SZ.
func pathValueKind(kind uint32) uint32 {
	if kind == pathKindSZ || kind == pathKindExpandSZ {
		return kind
	}
	return pathKindExpandSZ
}

// taskAction returns the exact user-logon task action: the quoted sidravia.exe
// path followed by the daemon start command with the resolved log level. The
// backslash join is deliberate so the action is deterministic on every build
// platform.
func taskAction(exeDir, logLevel string) string {
	if logLevel == "" {
		logLevel = installLogDefault
	}
	return `"` + normalizePathEntry(exeDir) + `\sidravia.exe" daemon start --log-level ` + logLevel
}

// schTasksArgs returns the exact schtasks.exe argument sets for create, query
// and delete of the named task with the given action. Existence is decided by
// exit code only; schtasks output is never parsed.
func schTasksArgs(action, name string) (create, query, delete []string) {
	return []string{"/Create", "/F", "/TN", name, "/TR", action, "/SC", "ONLOGON"},
		[]string{"/Query", "/TN", name},
		[]string{"/Delete", "/TN", name, "/F"}
}

// runInstall registers exeDir in the per-user PATH and creates the exact
// user-logon task. A PATH write broadcasts a best-effort environment change
// notification; task creation is required.
func runInstall(sys installSystem, exeDir, logLevel string) error {
	value, kind, err := sys.readUserPath()
	if err != nil {
		return err
	}
	if next := pathWithEntry(value, exeDir); next != value {
		if err := sys.writeUserPath(next, pathValueKind(kind)); err != nil {
			return err
		}
		_ = sys.broadcastEnvironmentChange()
	}
	return sys.createTask(installTaskName, taskAction(exeDir, logLevel))
}

// runUninstall removes the exact PATH entry for exeDir and deletes the
// user-logon task. It never touches Configuration, credentials, Profile or log
// data.
func runUninstall(sys installSystem, exeDir string) error {
	value, kind, err := sys.readUserPath()
	if err != nil {
		return err
	}
	if next, changed := pathWithoutEntry(value, exeDir); changed {
		if err := sys.writeUserPath(next, pathValueKind(kind)); err != nil {
			return err
		}
		_ = sys.broadcastEnvironmentChange()
	}
	exists, err := sys.taskExists(installTaskName)
	if err != nil {
		return err
	}
	if exists {
		return sys.deleteTask(installTaskName)
	}
	return nil
}

// resolveInstallDirectory resolves the installed-mode executable directory and
// refuses portable mode, which is self-contained and never system-integrated.
func resolveInstallDirectory(resolve func() (productlayout.Layout, error)) (string, error) {
	layout, err := resolve()
	if err != nil {
		return "", err
	}
	if layout.Mode == productlayout.ModePortable {
		return "", errors.New("便携模式不支持安装集成")
	}
	return layout.ExecutableDirectory, nil
}

// verifyInstallDirectory requires the sibling daemon executable so install
// registers a complete product directory.
func verifyInstallDirectory(exeDir string, stat func(string) (os.FileInfo, error)) error {
	const daemonSibling = "sidraviad.exe"
	if _, err := stat(filepath.Join(exeDir, daemonSibling)); err != nil {
		return fmt.Errorf("安装目录缺少 %s: %w", daemonSibling, err)
	}
	return nil
}
