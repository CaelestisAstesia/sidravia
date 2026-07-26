//go:build !windows

package cli

import (
	"errors"
	"io"
)

func readInteractivePassword(input io.Reader, errorOutput io.Writer) (string, error) {
	return "", errors.New("interactive password input is unsupported on this platform; use --password-stdin")
}
