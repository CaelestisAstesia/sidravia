//go:build windows

package setup

import (
	"golang.org/x/sys/windows"
	"path/filepath"
)

func plainPath(path string) error {
	for {
		p, e := windows.UTF16PtrFromString(path)
		if e != nil {
			return e
		}
		a, e := windows.GetFileAttributes(p)
		if e != nil {
			return e
		}
		if a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return ErrChanged
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
