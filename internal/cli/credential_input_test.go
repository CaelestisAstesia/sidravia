package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"sidravia/internal/cli/internal/daemonconn"
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

func TestCLIRejectsInvalidCredentialArguments(t *testing.T) {
	for _, bad := range []string{" bad", "bad ", "a b", "中文", "全角！"} {
		deps := authDependencies{connection: daemonconn.Connection{Acquire: func(context.Context) (daemonconn.Client, error) {
			t.Fatal("invalid input reached daemon")
			return nil, nil
		}}}
		if err := runAuthStart(authStartOptions{username: bad}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
		if err := runConfigCreate(configCreateOptions{username: bad}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
		if err := runConfigUpdate(configUpdateOptions{username: &bad}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
		deps.readStdinPassword = func(io.Reader) (string, error) { return bad, nil }
		if err := runConfigSetPassword(configPasswordOptions{passwordStdin: true}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
