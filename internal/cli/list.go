package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

type listDependencies struct {
	connection daemonConnectionDependencies
	stdout     io.Writer
}

func defaultListDependencies(identity clientbootstrap.Identity) listDependencies {
	return listDependencies{
		connection: defaultDaemonConnectionDependencies(identity),
		stdout:     os.Stdout,
	}
}

func authList(identity clientbootstrap.Identity) error {
	return runAuthList(defaultListDependencies(identity))
}

func profileList(identity clientbootstrap.Identity) error {
	return runProfileList(defaultListDependencies(identity))
}

func runAuthList(deps listDependencies) error {
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		response, err := callList(connection, deps.connection.callTimeout, contract.MethodSessionList)
		if err != nil {
			return wrapSafeOperation("列出 Session", err)
		}
		result, err := decodeSessionListResult(response)
		if err != nil {
			return wrapSafeOperation("解码 Session 列表", err)
		}
		return writeSessionList(deps.stdout, result)
	})
}

func runProfileList(deps listDependencies) error {
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		response, err := callList(connection, deps.connection.callTimeout, contract.MethodProfileList)
		if err != nil {
			return wrapSafeOperation("列出 Profile", err)
		}
		result, err := decodeProfileListResult(response)
		if err != nil {
			return wrapSafeOperation("解码 Profile 列表", err)
		}
		return writeProfileList(deps.stdout, result)
	})
}

// callList sends the canonical empty JSON object for a list method. List
// commands are read-only queries and never call daemon lifecycle methods.
func callList(connection daemonClient, timeout time.Duration, method string) ([]byte, error) {
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

func decodeSessionListResult(data []byte) (contract.SessionListResult, error) {
	var result contract.SessionListResult
	if err := decodeStrictList(data, &result); err != nil {
		return contract.SessionListResult{}, err
	}
	if result.Sessions == nil {
		return contract.SessionListResult{}, fmt.Errorf("Session 列表缺少 sessions 字段")
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

func decodeStrictList(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("列表结果之后存在多余数据")
		}
		return fmt.Errorf("列表结果之后存在多余数据")
	}
	return nil
}

func writeSessionList(output io.Writer, result contract.SessionListResult) error {
	p := newPresentation(output)
	return wrapSafeOperation("写入 Session 列表", p.complete(renderSessionList(p, &result)))
}

func writeProfileList(output io.Writer, result contract.ProfileListResult) error {
	p := newPresentation(output)
	return wrapSafeOperation("写入 Profile 列表", p.complete(renderProfileList(p, &result)))
}
