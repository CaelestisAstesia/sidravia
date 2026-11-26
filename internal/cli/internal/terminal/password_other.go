//go:build !windows && !linux

package terminal

import (
	"errors"
	"io"
)

func ReadPassword(input io.Reader, errorOutput io.Writer) (string, error) {
	return "", errors.New("当前平台不支持交互式密码输入；请使用 --password-stdin")
}

func IsConsole(io.Reader) bool { return false }
