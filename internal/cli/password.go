package cli

import (
	"errors"
	"io"
)

const maxPasswordBytes = 4096

type safeOperationError struct {
	label string
	cause error
}

func (err safeOperationError) Error() string {
	return err.label
}

func (err safeOperationError) Unwrap() error {
	return err.cause
}

func wrapSafeOperation(label string, cause error) error {
	if cause == nil {
		return nil
	}
	return safeOperationError{label: label, cause: cause}
}

func readPasswordStdin(input io.Reader) (string, error) {
	password, err := readPasswordLine(input)
	if err != nil {
		if errors.Is(err, errPasswordTooLong) {
			return "", err
		}
		return "", wrapSafeOperation("read password from stdin", err)
	}
	return password, nil
}

var errPasswordTooLong = errors.New("password input exceeds 4096 bytes")

func readPasswordLine(input io.Reader) (string, error) {
	line := make([]byte, 0, maxPasswordBytes+2)
	var one [1]byte

	for {
		n, err := input.Read(one[:])
		if n > 0 {
			line = append(line, one[0])
			if one[0] == '\n' {
				break
			}
			if len(line) >= maxPasswordBytes+2 {
				return "", errPasswordTooLong
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
		if n == 0 {
			return "", io.ErrNoProgress
		}
	}

	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
	}
	if len(line) > maxPasswordBytes {
		return "", errPasswordTooLong
	}
	return string(line), nil
}

func readHiddenPassword(
	input io.Reader,
	errorOutput io.Writer,
	echoInputFlag uint32,
	getMode func() (uint32, error),
	setMode func(uint32) error,
) (string, error) {
	if err := writeAll(errorOutput, "Password: "); err != nil {
		return "", wrapSafeOperation("write password prompt", err)
	}

	originalMode, err := getMode()
	if err != nil {
		return "", wrapSafeOperation("get console mode", err)
	}
	if err := setMode(originalMode &^ echoInputFlag); err != nil {
		return "", wrapSafeOperation("disable console echo", err)
	}

	password, readErr := readPasswordLine(input)
	lineBreakErr := writeAll(errorOutput, "\n")
	restoreErr := setMode(originalMode)

	var resultErrors []error
	if readErr != nil {
		resultErrors = append(resultErrors, wrapSafeOperation("read interactive password", readErr))
	}
	if lineBreakErr != nil {
		resultErrors = append(resultErrors, wrapSafeOperation("write password line break", lineBreakErr))
	}
	if restoreErr != nil {
		resultErrors = append(resultErrors, wrapSafeOperation("restore console mode", restoreErr))
	}
	if len(resultErrors) != 0 {
		return "", errors.Join(resultErrors...)
	}
	return password, nil
}

func writeAll(writer io.Writer, value string) error {
	written, err := io.WriteString(writer, value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}
