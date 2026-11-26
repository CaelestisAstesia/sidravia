package authcmd

import (
	"io"

	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

func List(connectionDeps daemonconn.Connection, output io.Writer) error {
	return daemonconn.With(connectionDeps, func(connection daemonconn.Client) error {
		response, err := daemonconn.CallQuery(connection, connectionDeps.Timeout, contract.MethodSessionList)
		if err != nil {
			return terminal.Failure("列出 Session", err)
		}
		result, err := contract.DecodeSessionListResult(response)
		if err != nil {
			return terminal.Failure("解码 Session 列表", err)
		}
		return terminal.WriteSessionList(output, result)
	})
}
