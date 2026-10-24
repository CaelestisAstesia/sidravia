//go:build windows

// Package installationgate shares the current-user Inno exclusion boundary.
package installationgate

import (
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
)

var ErrBusy = errors.New("安装或更新正在进行，请完成或取消后再启动程序")

// AcquireStart serializes creation with the installer's mutex. Release must be
// called on the same goroutine; Windows mutex ownership is thread-affine.
func AcquireStart() (func(), error) {
	runtime.LockOSThread()
	fail := func(err error) (func(), error) { runtime.UnlockOSThread(); return nil, err }
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fail(err)
	}
	name, err := windows.UTF16PtrFromString("Global\\SidraviaInstall-" + user.User.Sid.String())
	if err != nil {
		return fail(err)
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return fail(err)
	}
	result, err := windows.WaitForSingleObject(h, 0)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		windows.CloseHandle(h)
		return fail(ErrBusy)
	}
	return func() { windows.ReleaseMutex(h); windows.CloseHandle(h); runtime.UnlockOSThread() }, nil
}
