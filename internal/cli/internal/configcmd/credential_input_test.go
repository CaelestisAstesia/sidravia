package configcmd

import (
	"context"
	"errors"
	"io"
	"testing"

	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/connection/credential/input"
)

func TestConfigRejectsInvalidCredentials(t *testing.T) {
	for _, bad := range []string{" bad", "bad ", "a b", "中文", "全角！"} {
		deps := dependencies{connection: daemonconn.Connection{Acquire: func(context.Context) (daemonconn.Client, error) {
			t.Fatal("invalid input reached daemon")
			return nil, nil
		}}}
		if err := runConfigCreate(CreateOptions{Username: bad}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
		if err := runConfigUpdate(UpdateOptions{Username: &bad}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
		deps.ReadStdinPassword = func(io.Reader) (string, error) { return bad, nil }
		if err := runConfigSetPassword(PasswordOptions{PasswordStdin: true}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
