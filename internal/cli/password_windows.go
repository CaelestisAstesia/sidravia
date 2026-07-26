//go:build windows

package cli

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func readInteractivePassword(input io.Reader, errorOutput io.Writer) (string, error) {
	file, ok := input.(*os.File)
	if !ok || file != os.Stdin {
		return "", errors.New("interactive password input requires the Windows console; use --password-stdin")
	}

	handle := windows.Handle(file.Fd())
	return readHiddenPassword(
		input,
		errorOutput,
		uint32(windows.ENABLE_ECHO_INPUT),
		func() (uint32, error) {
			var mode uint32
			if err := windows.GetConsoleMode(handle, &mode); err != nil {
				return 0, err
			}
			return mode, nil
		},
		func(mode uint32) error {
			return windows.SetConsoleMode(handle, mode)
		},
	)
}
