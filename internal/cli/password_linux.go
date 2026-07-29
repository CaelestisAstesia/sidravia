//go:build linux

package cli

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// termiosGetter reads the current terminal state. Tests inject a private
// replacement; the production path uses unix.IoctlGetTermios.
type termiosGetter func(fd int, req uint) (*unix.Termios, error)

// termiosSetter writes the terminal state. Tests inject a private replacement;
// the production path uses unix.IoctlSetTermios.
type termiosSetter func(fd int, req uint, value *unix.Termios) error

const linuxNonTerminalMessage = "交互式密码输入需要终端；请使用 --password-stdin"

// readInteractivePassword reads a hidden password from an interactive Linux
// terminal. It accepts an *os.File terminal, saves the complete termios state,
// prints the existing `密码： ` prompt to stderr, clears only ECHO, reads
// through readPasswordLine, prints one newline, then restores the exact
// original state. Read, newline and restore failures are joined so errors.Is
// can observe every cause. A non-file or non-terminal input returns a fixed
// Chinese instruction to use --password-stdin without reading password bytes
// or echoing a secret.
func readInteractivePassword(input io.Reader, errorOutput io.Writer) (string, error) {
	return readInteractivePasswordWith(input, errorOutput, unix.IoctlGetTermios, unix.IoctlSetTermios)
}

func readInteractivePasswordWith(input io.Reader, errorOutput io.Writer, get termiosGetter, set termiosSetter) (string, error) {
	file, ok := input.(*os.File)
	if !ok {
		return "", errors.New(linuxNonTerminalMessage)
	}
	fd := int(file.Fd())

	original, err := get(fd, unix.TCGETS)
	if err != nil {
		return "", errors.New(linuxNonTerminalMessage)
	}

	if err := writeAll(errorOutput, "密码： "); err != nil {
		return "", wrapSafeOperation("写入密码提示", err)
	}

	modified := *original
	modified.Lflag &^= unix.ECHO
	if err := set(fd, unix.TCSETS, &modified); err != nil {
		return "", wrapSafeOperation("关闭控制台回显", err)
	}

	password, readErr := readPasswordLine(input)
	lineBreakErr := writeAll(errorOutput, "\n")
	restoreErr := set(fd, unix.TCSETS, original)

	var resultErrors []error
	if readErr != nil {
		resultErrors = append(resultErrors, wrapSafeOperation("读取交互式密码", readErr))
	}
	if lineBreakErr != nil {
		resultErrors = append(resultErrors, wrapSafeOperation("写入密码换行", lineBreakErr))
	}
	if restoreErr != nil {
		resultErrors = append(resultErrors, wrapSafeOperation("恢复控制台模式", restoreErr))
	}
	if len(resultErrors) != 0 {
		return "", errors.Join(resultErrors...)
	}
	return password, nil
}
