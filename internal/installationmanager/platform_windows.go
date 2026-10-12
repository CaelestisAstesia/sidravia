//go:build windows

package installationmanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"sidravia/internal/productlayout"
)

type registryPath struct{}

func (registryPath) read() (pathValue, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return pathValue{}, nil
	}
	if err != nil {
		return pathValue{}, err
	}
	defer k.Close()
	s, kind, err := k.GetStringValue("Path")
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return pathValue{}, nil
	}
	if err != nil {
		return pathValue{}, err
	}
	if kind != registry.SZ && kind != registry.EXPAND_SZ {
		return pathValue{}, errors.New("unsupported PATH registry type")
	}
	return pathValue{text: s, kind: kind, exists: true}, nil
}
func (registryPath) write(v pathValue) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, "Environment", registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if v.kind == registry.EXPAND_SZ {
		return k.SetExpandStringValue("Path", v.text)
	}
	return k.SetStringValue("Path", v.text)
}
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
	if operation == PrepareUninstall || operation == CheckUninstall || operation == DeleteData {
		result = Result{Version: 1, Directory: directory}
		blocked, guardErr := productRunning(directory, operation == DeleteData)
		if guardErr != nil {
			err = &Failure{Code: "process_query_failed", Cause: guardErr}
		} else if blocked {
			err = &Failure{Code: "application_running", Cause: errors.New("product must exit before cleanup")}
		} else if operation == CheckUninstall {
			result, err = taskOperation(directory, Status)
			if err == nil && result.Autostart == "conflict" {
				err = &Failure{Code: "scheduled_task_conflict", Cause: errors.New("foreign startup task")}
			}
		} else if operation == PrepareUninstall {
			result, err = taskOperation(directory, DisableAutostart)
			if err == nil {
				taskChanged := result.Changed
				result, err = operate(registryPath{}, directory, DisablePath)
				result.Changed = result.Changed || taskChanged
				result.Autostart = "disabled"
			}
		} else {
			layout, layoutErr := productlayout.Resolve()
			if layoutErr != nil {
				err = &Failure{Code: "data_location_failed", Cause: layoutErr}
			} else if layout.Mode != productlayout.ModeInstalled {
				err = &Failure{Code: "portable_data_removal_unsupported", Cause: errors.New("portable data is preserved")}
			} else {
				result.Changed, layoutErr = removeData(filepath.Dir(layout.ConfigurationsPath), filepath.Dir(layout.RuntimeInfoPath))
				if layoutErr != nil {
					err = &Failure{Code: "data_deletion_failed", Cause: layoutErr}
				} else {
					result.OK = true
					result.Code = "ok"
				}
			}
		}
		if err != nil {
			result.OK = false
			result.Code = Code(err)
		}
	} else if operation == Status || operation == EnablePath || operation == DisablePath {
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
	result.Capabilities = []string{"user_path", "user_autostart", "production_data_removal"}
	result.GUIAvailable = guiAvailable
	return result, err
}
