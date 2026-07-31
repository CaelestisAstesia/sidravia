//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"sidravia/internal/productlayout"
)

// windowsInstallSystem is the real platform seam: the HKCU\Environment PATH
// value and the Task Scheduler task managed through schtasks.exe.
type windowsInstallSystem struct{}

// readUserPath reads the per-user PATH value and its registry kind. A missing
// value is reported as empty with kind 0.
func (windowsInstallSystem) readUserPath() (string, uint32, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", 0, err
	}
	defer key.Close()
	value, kind, err := key.GetStringValue(pathValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	return value, kind, nil
}

// writeUserPath writes the per-user PATH value preserving the passed kind.
func (windowsInstallSystem) writeUserPath(value string, kind uint32) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if kind == pathKindExpandSZ {
		return key.SetExpandStringValue(pathValueName, value)
	}
	return key.SetStringValue(pathValueName, value)
}

// broadcastEnvironmentChange notifies running processes that the user
// environment changed, so processes spawned from the existing shell session
// resolve the updated PATH. It is best-effort by the caller.
func (windowsInstallSystem) broadcastEnvironmentChange() error {
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return err
	}
	result, _, callErr := procSendMessageTimeoutW.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung,
		5000,
		0,
	)
	if result == 0 {
		return fmt.Errorf("user32 SendMessageTimeoutW: %w", callErr)
	}
	return nil
}

// taskExists reports whether the named task exists by schtasks exit code only.
// Any nonzero exit means the task is absent; a Start failure is a real error.
func (windowsInstallSystem) taskExists(name string) (bool, error) {
	_, query, _ := schTasksArgs("", name)
	err := runSchTasks(query)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

// createTask creates or replaces the named task with the exact action.
func (windowsInstallSystem) createTask(name, action string) error {
	create, _, _ := schTasksArgs(action, name)
	return runSchTasks(create)
}

// deleteTask deletes the named task.
func (windowsInstallSystem) deleteTask(name string) error {
	_, _, del := schTasksArgs("", name)
	return runSchTasks(del)
}

// runSchTasks runs schtasks.exe with args and preserves the command cause and a
// bounded output excerpt for diagnostics. Only the exit status is used for
// decisions; the output is never shown to the user.
func runSchTasks(args []string) error {
	cmd := exec.Command("schtasks.exe", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks: %w: %s", err, boundedOutput(out))
	}
	return nil
}

func boundedOutput(out []byte) string {
	s := string(out)
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// User32 SendMessageTimeoutW is not exposed by golang.org/x/sys/windows, so the
// broadcast is declared through a system DLL without adding a dependency.
var (
	procSendMessageTimeoutW = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

	hwndBroadcast   = uintptr(0xFFFF)
	wmSettingChange = uintptr(0x001A)
	smtoAbortIfHung = uintptr(0x0002)
)

// runInstallCommand registers the installed-mode executable directory in the
// per-user PATH and creates the exact user-logon task.
func runInstallCommand(logLevel string) error {
	dir, err := resolveInstallDirectory(productlayout.Resolve)
	if err != nil {
		return err
	}
	if err := verifyInstallDirectory(dir, os.Stat); err != nil {
		return err
	}
	return runInstall(windowsInstallSystem{}, dir, logLevel)
}

// runUninstallCommand removes the exact PATH entry and deletes the user-logon
// task without touching any data.
func runUninstallCommand() error {
	dir, err := resolveInstallDirectory(productlayout.Resolve)
	if err != nil {
		return err
	}
	return runUninstall(windowsInstallSystem{}, dir)
}
