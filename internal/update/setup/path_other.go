//go:build !windows

package setup

import "os"

func plainPath(path string) error {
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if i.Mode()&os.ModeSymlink != 0 {
		return ErrChanged
	}
	return nil
}
