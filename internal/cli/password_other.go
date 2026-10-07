//go:build !windows && !linux

package cli

import (
	"errors"
	"io"
)

func readInteractivePassword(input io.Reader, errorOutput io.Writer) (string, error) {
	return "", errors.New("当前平台不支持交互式密码输入；请使用 --password-stdin")
}

func isConsoleInput(io.Reader) bool { return false }
