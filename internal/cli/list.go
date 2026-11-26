package cli

import (
	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

func profileList(identity daemonctl.Identity) error {
	return runProfileList(defaultReadOnlyCommandDependencies(identity))
}

func runProfileList(deps readOnlyCommandDependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		response, err := daemonconn.CallQuery(connection, deps.connection.Timeout, contract.MethodProfileList)
		if err != nil {
			return terminal.Failure("列出 Profile", err)
		}
		result, err := contract.DecodeProfileListResult(response)
		if err != nil {
			return terminal.Failure("解码 Profile 列表", err)
		}
		return terminal.WriteProfileList(deps.stdout, result)
	})
}
