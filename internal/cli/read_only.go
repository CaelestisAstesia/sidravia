package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"sidravia/internal/background/daemonctl"
)

type readOnlyCommandDependencies struct {
	connection daemonConnectionDependencies
	stdout     io.Writer
}

func defaultReadOnlyCommandDependencies(identity daemonctl.Identity) readOnlyCommandDependencies {
	return readOnlyCommandDependencies{
		connection: defaultReadOnlyDaemonConnectionDependencies(identity),
		stdout:     os.Stdout,
	}
}

// callReadOnlyQuery sends the canonical empty object for read-only queries.
// Its callers never acquire or start a new daemon generation.
func callReadOnlyQuery(connection daemonClient, timeout time.Duration, method string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := connection.Call(ctx, method, json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		return nil, errors.New(ipcErrorText(code))
	}
	return response.Result, nil
}
