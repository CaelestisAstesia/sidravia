package authcmd

import (
	"context"
	"errors"
	"testing"

	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/connection/credential/input"
)

func TestAuthRejectsInvalidCredentials(t *testing.T) {
	for _, bad := range []string{" bad", "bad ", "a b", "中文", "全角！"} {
		deps := dependencies{connection: daemonconn.Connection{Acquire: func(context.Context) (daemonconn.Client, error) {
			t.Fatal("invalid input reached daemon")
			return nil, nil
		}}}
		if err := runAuthStart(StartOptions{Username: bad}, deps); !errors.Is(err, input.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
