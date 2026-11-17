//go:build windows

package setup

import (
	"golang.org/x/sys/windows"
	"os"
)

func singleLink(f *os.File) error {
	var info windows.ByHandleFileInformation
	if e := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); e != nil {
		return e
	}
	if info.NumberOfLinks != 1 {
		return ErrChanged
	}
	return nil
}
