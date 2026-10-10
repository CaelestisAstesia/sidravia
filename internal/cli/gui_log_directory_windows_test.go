//go:build windows

package cli

import (
	"golang.org/x/sys/windows"
	"testing"
)

func TestGUILogNativeUnicodeVerbReturnAndCOMBalance(t *testing.T) {
	for _, hr := range []uint32{0, 1, 0x80010106} {
		for _, result := range []uintptr{0, 32, 33} {
			initialized, uninitialized, executed := 0, 0, 0
			err := openLogDirectoryNativeWith(`C:\日志\当前实例`, guiLogNativeCalls{
				initialize: func() uint32 { initialized++; return hr }, uninitialize: func() { uninitialized++ }, execute: func(verb, path *uint16) uintptr {
					executed++
					if windows.UTF16PtrToString(verb) != "open" || windows.UTF16PtrToString(path) != `C:\日志\当前实例` {
						t.Fatal("Unicode arguments changed")
					}
					return result
				},
			})
			if initialized != 1 {
				t.Fatal("COM not initialized")
			}
			success := hr <= 1 && result > 32
			if (err == nil) != success {
				t.Fatalf("hr=%x result=%d err=%v", hr, result, err)
			}
			if hr <= 1 {
				if executed != 1 || uninitialized != 1 {
					t.Fatal("unbalanced initialized COM")
				}
			} else if executed != 0 || uninitialized != 0 {
				t.Fatal("failed COM called shell/uninitialize")
			}
		}
	}
}
