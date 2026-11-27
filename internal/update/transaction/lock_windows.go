//go:build windows

package transaction

import (
	"errors"

	"golang.org/x/sys/windows"
)

func lock(path string) (func(), error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.Join(ErrConflict, err)
		}
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		windows.CloseHandle(handle)
		return nil, ErrInvalid
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
