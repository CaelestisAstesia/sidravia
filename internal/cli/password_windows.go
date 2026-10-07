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
		return "", errors.New("交互式密码输入需要 Windows 控制台；请使用 --password-stdin")
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

func isConsoleInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(file.Fd()), &mode) == nil
}
