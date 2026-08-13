package clientbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sidravia/internal/ipc/contract"
)

// These focused test collaborators retain the former read-only launcher
// regression without widening the production API.
type daemonClient interface {
	Call(context.Context, string, json.RawMessage) (contract.Response, error)
	Close() error
}
type probeDependencies struct {
	runtimeInfoPath func() (string, error)
	readRuntimeInfo func(string) (contract.RuntimeInfo, error)
	connect         func(context.Context, contract.RuntimeInfo) (daemonClient, error)
	callTimeout     interface{}
}

func runDaemonStatus(deps probeDependencies, output io.Writer) error {
	path, err := deps.runtimeInfoPath()
	if err != nil {
		return err
	}
	_, err = deps.readRuntimeInfo(path)
	if errors.Is(err, os.ErrNotExist) {
		_, err = io.WriteString(output, "守护进程：已停止（stopped）\n")
		return err
	}
	return err
}
