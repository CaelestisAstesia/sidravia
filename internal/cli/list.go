package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"sidravia/internal/ipc/contract"
)

type listDependencies struct {
	connection daemonConnectionDependencies
	stdout     io.Writer
}

func defaultListDependencies() listDependencies {
	return listDependencies{
		connection: defaultDaemonConnectionDependencies(),
		stdout:     os.Stdout,
	}
}

func authList() error {
	return runAuthList(defaultListDependencies())
}

func profileList() error {
	return runProfileList(defaultListDependencies())
}

func runAuthList(deps listDependencies) error {
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		response, err := callList(connection, deps.connection.callTimeout, contract.MethodSessionList)
		if err != nil {
			return wrapSafeOperation("list Sessions", err)
		}
		result, err := decodeSessionListResult(response)
		if err != nil {
			return wrapSafeOperation("decode Session list", err)
		}
		return writeSessionList(deps.stdout, result)
	})
}

func runProfileList(deps listDependencies) error {
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		response, err := callList(connection, deps.connection.callTimeout, contract.MethodProfileList)
		if err != nil {
			return wrapSafeOperation("list Profiles", err)
		}
		result, err := decodeProfileListResult(response)
		if err != nil {
			return wrapSafeOperation("decode Profile list", err)
		}
		return writeProfileList(deps.stdout, result)
	})
}

func callList(connection daemonClient, timeout time.Duration, method string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := connection.Call(ctx, method, json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}
	if !response.OK {
		if response.Error == nil {
			return nil, fmt.Errorf("daemon returned malformed list error")
		}
		return nil, fmt.Errorf("daemon list error %s", response.Error.Code)
	}
	return response.Result, nil
}

func decodeSessionListResult(data []byte) (contract.SessionListResult, error) {
	var result contract.SessionListResult
	if err := decodeStrictList(data, &result); err != nil {
		return contract.SessionListResult{}, err
	}
	if result.Sessions == nil {
		return contract.SessionListResult{}, fmt.Errorf("Session list is missing sessions")
	}
	for _, session := range result.Sessions {
		if err := validateSessionResult(session); err != nil {
			return contract.SessionListResult{}, err
		}
	}
	return result, nil
}

func decodeProfileListResult(data []byte) (contract.ProfileListResult, error) {
	var result contract.ProfileListResult
	if err := decodeStrictList(data, &result); err != nil {
		return contract.ProfileListResult{}, err
	}
	if result.Profiles == nil {
		return contract.ProfileListResult{}, fmt.Errorf("Profile list is missing profiles")
	}
	for _, profile := range result.Profiles {
		if profile.InstitutionProfileID == "" ||
			profile.DisplayName == "" ||
			profile.AuthenticationProtocolID == "" {
			return contract.ProfileListResult{}, fmt.Errorf("Profile list contains incomplete profile")
		}
	}
	return result, nil
}

func decodeStrictList(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing list result")
		}
		return fmt.Errorf("trailing list result data")
	}
	return nil
}

func writeSessionList(output io.Writer, result contract.SessionListResult) error {
	var block bytes.Buffer
	if len(result.Sessions) == 0 {
		block.WriteString("没有 Session。\n")
	} else {
		fmt.Fprintf(&block, "会话（%d）：\n", len(result.Sessions))
		for _, session := range result.Sessions {
			profile := session.InstitutionProfileID
			if session.InstitutionDisplayName != "" {
				profile = fmt.Sprintf("%s (%s)", session.InstitutionDisplayName, session.InstitutionProfileID)
			}
			fmt.Fprintf(
				&block,
				"- %s | 状态: %s | 机构: %s | 账号: %s | 更新时间: %s\n",
				session.AuthenticationSessionID,
				session.State,
				profile,
				session.AccountLabel,
				session.UpdatedAt,
			)
		}
	}
	if err := writeAll(output, block.String()); err != nil {
		return wrapSafeOperation("write Session list", err)
	}
	return nil
}

func writeProfileList(output io.Writer, result contract.ProfileListResult) error {
	var block bytes.Buffer
	if len(result.Profiles) == 0 {
		block.WriteString("没有可用的机构 Profile。\n")
	} else {
		fmt.Fprintf(&block, "机构 Profile（%d）：\n", len(result.Profiles))
		for _, profile := range result.Profiles {
			fmt.Fprintf(
				&block,
				"- %s | 名称: %s | 协议: %s\n",
				profile.InstitutionProfileID,
				profile.DisplayName,
				profile.AuthenticationProtocolID,
			)
		}
	}
	if err := writeAll(output, block.String()); err != nil {
		return wrapSafeOperation("write Profile list", err)
	}
	return nil
}
