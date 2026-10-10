//go:build windows

package cli

import (
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"unsafe"
)

var guiLogOle32 = windows.NewLazySystemDLL("ole32.dll")
var guiLogShell32 = windows.NewLazySystemDLL("shell32.dll")
var guiLogInitialize = guiLogOle32.NewProc("CoInitializeEx")
var guiLogUninitialize = guiLogOle32.NewProc("CoUninitialize")
var guiLogExecute = guiLogShell32.NewProc("ShellExecuteW")

type guiLogNativeCalls struct {
	initialize   func() uint32
	uninitialize func()
	execute      func(*uint16, *uint16) uintptr
}

func openLogDirectoryNative(path string) error {
	return openLogDirectoryNativeWith(path, guiLogNativeCalls{
		initialize:   func() uint32 { r, _, _ := guiLogInitialize.Call(0, 2); return uint32(r) },
		uninitialize: func() { guiLogUninitialize.Call() },
		execute: func(verb, target *uint16) uintptr {
			r, _, _ := guiLogExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
			return r
		},
	})
}
func openLogDirectoryNativeWith(path string, calls guiLogNativeCalls) error {
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr := calls.initialize()
	if hr != 0 && hr != 1 {
		return fmt.Errorf("initialize log directory COM: HRESULT 0x%08x", hr)
	}
	defer calls.uninitialize()
	result := calls.execute(verb, target)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
	if result <= 32 {
		return fmt.Errorf("open log directory: %w", syscall.Errno(result))
	}
	return nil
}
