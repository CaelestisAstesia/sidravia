//go:build windows

package setup

import (
	"errors"
	"os"

	"sidravia/internal/process/winproc"
)

func singleLink(file *os.File) error {
	err := winproc.CheckSingleLink(file)
	if errors.Is(err, winproc.ErrImageChanged) {
		return ErrChanged
	}
	return err
}
