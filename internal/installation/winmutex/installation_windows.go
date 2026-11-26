//go:build windows

// Current-user exclusion is shared by Inno, portable publication and startup.
package winmutex

import (
	"errors"
	"runtime"

	"golang.org/x/sys/windows"
)

var ErrInstallationBusy = errors.New("安装或更新正在进行，请完成或取消后再启动程序")

// AcquireInstallation serializes creation with the installer's mutex. Release must be
// called on the same goroutine; Windows mutex ownership is thread-affine.
func AcquireInstallation() (func(), error) {
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
		return fail(ErrInstallationBusy)
	}
	return func() { windows.ReleaseMutex(h); windows.CloseHandle(h); runtime.UnlockOSThread() }, nil
}
