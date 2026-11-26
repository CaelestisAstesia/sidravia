package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/connection/credential/input"
)

func TestCredentialPromptsDoNotTrimOrEchoInvalidInput(t *testing.T) {
	for _, value := range []string{" user", "user ", "a b", "中文", "ａｂ！", "p\tass"} {
		var output bytes.Buffer
		_, err := terminal.ReadCredential(strings.NewReader(value+"\r\n"), &output, "账号：")
		if !errors.Is(err, input.ErrInvalid) || strings.Contains(output.String(), value) {
			t.Fatal("invalid credential altered or echoed")
		}
	}
	var output bytes.Buffer
	value, err := terminal.ReadCredential(strings.NewReader("Ab@9!\\\"\r\n"), &output, "账号：")
	if err != nil || value != "Ab@9!\\\"" {
		t.Fatal("valid symbols changed")
	}
}
