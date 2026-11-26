package cli

import (
	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

func networkInterfaces(identity daemonctl.Identity) error {
	return runNetworkInterfaces(defaultReadOnlyCommandDependencies(identity))
}
func runNetworkInterfaces(deps readOnlyCommandDependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		response, err := daemonconn.CallQuery(connection, deps.connection.Timeout, contract.MethodNetworkInterfaces)
		if err != nil {
			return terminal.Failure("查询网卡", err)
		}
		result, err := contract.DecodeNetworkInterfacesResult(response)
		if err != nil {
			return terminal.Failure("解码网卡列表", err)
		}
		p := terminal.NewPresentation(deps.stdout)
		return terminal.Failure("写入网卡列表", p.Complete(terminal.RenderNetworkInterfaces(result)))
	})
}
