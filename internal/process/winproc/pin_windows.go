//go:build windows

package winproc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

var ErrImageChanged = errors.New("Windows executable identity changed")

// Holding this ordinary read handle denies write/delete sharing until handoff;
// neither a mutable cache path nor a replaced old helper can change the launch.
func PinExecutable(path string, size int64, hash string) (*os.File, error) {
	if err := CheckPlainPath(path); err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		f.Close()
		return nil, ErrImageChanged
	}
	if err := CheckSingleLink(f); err != nil {
		f.Close()
		return nil, err
	}
	hsh := sha256.New()
	n, err := io.Copy(hsh, io.LimitReader(f, size+1))
	if err != nil || n != size || hex.EncodeToString(hsh.Sum(nil)) != hash {
		f.Close()
		return nil, ErrImageChanged
	}

	return f, nil
}

func CheckPlainPath(path string) error {
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
			return ErrImageChanged
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func CheckSingleLink(f *os.File) error {
	var info windows.ByHandleFileInformation
	if e := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); e != nil {
		return e
	}
	if info.NumberOfLinks != 1 {
		return ErrImageChanged
	}
	return nil
}
