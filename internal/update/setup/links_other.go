//go:build !windows

package setup

import (
	"os"
	"syscall"
)

func singleLink(f *os.File) error {
	i, e := f.Stat()
	if e != nil {
		return e
	}
	if s, ok := i.Sys().(*syscall.Stat_t); ok && s.Nlink != 1 {
		return ErrChanged
	}
	return nil
}
