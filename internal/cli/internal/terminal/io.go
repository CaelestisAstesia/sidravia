package terminal

import (
	"io"
	"os"

	"sidravia/internal/connection/credential/input"
)

type IO struct {
	Stdin                   io.Reader
	Stdout                  io.Writer
	Stderr                  io.Writer
	ReadStdinPassword       func(io.Reader) (string, error)
	ReadInteractivePassword func(io.Reader, io.Writer) (string, error)
	InputIsConsole          func(io.Reader) bool
}

func StandardIO() IO {
	return IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, ReadStdinPassword: ReadPasswordStdin, ReadInteractivePassword: ReadPassword, InputIsConsole: IsConsole}
}

func (terminal IO) ValidatedPassword(fromStdin bool) (string, error) {
	var password string
	var err error
	if fromStdin {
		password, err = terminal.ReadStdinPassword(terminal.Stdin)
	} else {
		password, err = terminal.ReadInteractivePassword(terminal.Stdin, terminal.Stderr)
	}
	if err != nil {
		return "", err
	}
	if err := input.ValidateNewText(password); err != nil {
		return "", err
	}
	return password, nil
}
