//go:build windows

package setup

import (
	"errors"

	"sidravia/internal/process/winproc"
)

func plainPath(path string) error {
	err := winproc.CheckPlainPath(path)
	if errors.Is(err, winproc.ErrImageChanged) {
		return ErrChanged
	}
	return err
}
