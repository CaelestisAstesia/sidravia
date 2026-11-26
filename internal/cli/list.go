package cli

import (
	"fmt"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

func authList(identity daemonctl.Identity) error {
	return runAuthList(defaultReadOnlyCommandDependencies(identity))
}

func profileList(identity daemonctl.Identity) error {
	return runProfileList(defaultReadOnlyCommandDependencies(identity))
}

func runAuthList(deps readOnlyCommandDependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		response, err := daemonconn.CallQuery(connection, deps.connection.Timeout, contract.MethodSessionList)
		if err != nil {
			return terminal.Failure("列出 Session", err)
		}
		result, err := decodeSessionListResult(response)
		if err != nil {
			return terminal.Failure("解码 Session 列表", err)
		}
		return terminal.WriteSessionList(deps.stdout, result)
	})
}

func runProfileList(deps readOnlyCommandDependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		response, err := daemonconn.CallQuery(connection, deps.connection.Timeout, contract.MethodProfileList)
		if err != nil {
			return terminal.Failure("列出 Profile", err)
		}
		result, err := decodeProfileListResult(response)
		if err != nil {
			return terminal.Failure("解码 Profile 列表", err)
		}
		return terminal.WriteProfileList(deps.stdout, result)
	})
}

func decodeSessionListResult(data []byte) (contract.SessionListResult, error) {
	return contract.DecodeSessionListResult(data)
}

func decodeProfileListResult(data []byte) (contract.ProfileListResult, error) {
	var result contract.ProfileListResult
	if err := decodeStrictCLI(data, &result, "列表结果之后存在多余数据"); err != nil {
		return contract.ProfileListResult{}, err
	}
	if result.Profiles == nil {
		return contract.ProfileListResult{}, fmt.Errorf("Profile 列表缺少 profiles 字段")
	}
	for _, profile := range result.Profiles {
		if profile.InstitutionProfileID == "" ||
			profile.DisplayName == "" ||
			profile.AuthenticationProtocolID == "" {
			return contract.ProfileListResult{}, fmt.Errorf("Profile 列表包含不完整的 Profile")
		}
	}
	return result, nil
}
