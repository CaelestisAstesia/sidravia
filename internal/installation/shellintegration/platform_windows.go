//go:build windows

package shellintegration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (registryPath) notify() error {
	name, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return err
	}
	var result uintptr
	r, _, err := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(0xffff, 0x001a, 0, uintptr(unsafe.Pointer(name)), 2, 2000, uintptr(unsafe.Pointer(&result)))
	runtime.KeepAlive(name)
	if r == 0 {
		return fmt.Errorf("environment broadcast: %w", err)
	}
	return nil
}

// Run serializes manager integration reads and writes for the current user.
// Other applications do not participate in this lock; a verification mismatch
// reports failure rather than overwriting their later edits with a rollback.
func Run(directory string, operation Operation) (Result, error) {
	if err := ensureManagerJob(); err != nil {
		return Result{Version: 1, Code: "integration_process_boundary_failed", Capabilities: []string{}}, &Failure{Code: "integration_process_boundary_failed", Cause: err}
	}
	// Windows mutex ownership belongs to an OS thread, not a goroutine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fail := func(err error) (Result, error) {
		return Result{Version: 1, Code: "integration_lock_failed", Capabilities: []string{"user_path"}}, &Failure{Code: "integration_lock_failed", Cause: err}
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fail(err)
	}
	name, err := windows.UTF16PtrFromString("Global\\SidraviaManager-" + user.User.Sid.String())
	if err != nil {
		return fail(err)
	}
	mutex, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fail(err)
	}
	defer windows.CloseHandle(mutex)
	wait, err := windows.WaitForSingleObject(mutex, 10000)
	if err != nil {
		return fail(err)
	}
	if wait != windows.WAIT_OBJECT_0 && wait != windows.WAIT_ABANDONED {
		return fail(errors.New("integration lock timed out"))
	}
	defer windows.ReleaseMutex(mutex)
	gui, guiErr := os.Stat(filepath.Join(directory, "Sidravia.exe"))
	guiAvailable := guiErr == nil && gui.Mode().IsRegular()
	var result Result
	if operation == Status || operation == EnablePath || operation == DisablePath {
		result, err = operate(registryPath{}, directory, operation)
		if err == nil && operation == Status {
			state, taskErr := taskOperation(directory, Status)
			result.Autostart = state.Autostart
			if taskErr != nil {
				result.OK = false
				result.Code = Code(taskErr)
				err = taskErr
			}
		}
	} else {
		result, err = taskOperation(directory, operation)
	}
	result.Capabilities = []string{"user_path", "user_autostart"}
	result.GUIAvailable = guiAvailable
	return result, err
}
