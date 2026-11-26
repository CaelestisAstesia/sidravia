package terminal

import (
	"errors"
	"io"
	"strings"

	credentialinput "sidravia/internal/connection/credential/input"
)

func Confirm(input io.Reader, output io.Writer, prompt string) (bool, error) {
	if err := Write(output, prompt); err != nil {
		return false, Failure("写入确认提示", err)
	}
	value, err := ReadLine(input)
	if err != nil {
		return false, Failure("读取确认", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true, nil
	case "", "n", "no":
		return false, nil
	default:
		return false, errors.New("确认输入无效")
	}
}

// Credential prompts preserve every character except the input line delimiter.
func ReadCredential(input io.Reader, output io.Writer, prompt string) (string, error) {
	if err := Write(output, prompt); err != nil {
		return "", Failure("写入交互提示", err)
	}
	value, err := ReadLine(input)
	if err != nil {
		return "", Failure("读取交互输入", err)
	}
	if err := credentialinput.ValidateNewText(value); err != nil {
		return "", err
	}
	return value, nil
}

func ReadPrompt(input io.Reader, output io.Writer, prompt string) (string, error) {
	if err := Write(output, prompt); err != nil {
		return "", Failure("写入交互提示", err)
	}
	value, err := ReadLine(input)
	if err != nil {
		return "", Failure("读取交互输入", err)
	}
	return strings.TrimSpace(value), nil
}

func ReadBool(input io.Reader, output io.Writer, prompt string, defaultValue bool) (bool, error) {
	if err := Write(output, prompt); err != nil {
		return false, Failure("写入布尔提示", err)
	}
	value, err := ReadLine(input)
	if err != nil {
		return false, Failure("读取布尔输入", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	case "":
		return defaultValue, nil
	default:
		return false, errors.New("布尔输入无效")
	}
}
