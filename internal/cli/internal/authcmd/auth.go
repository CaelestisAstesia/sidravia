package authcmd

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/bindingargs"
	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/connection/credential/input"
	"sidravia/internal/ipc/contract"
)

type dependencies struct {
	connection daemonconn.Connection
	terminal.IO
}

func commandDependencies(connection daemonconn.Connection) dependencies {
	return dependencies{connection: connection, IO: terminal.StandardIO()}
}

type StartOptions struct {
	Binding         bindingargs.Flags
	ProfileID       string
	Username        string
	SessionID       string
	ConfigurationID string
	PasswordStdin   bool
}

func Start(identity daemonctl.Identity, options StartOptions) error {
	return runAuthStart(options, commandDependencies(daemonconn.MayStart(identity)))
}

func Status(identity daemonctl.Identity, sessionID string) error {
	return runAuthStatus(sessionID, commandDependencies(daemonconn.Existing(identity)))
}

func Stop(identity daemonctl.Identity, sessionID string) error {
	return runAuthStop(sessionID, commandDependencies(daemonconn.MayStart(identity)))
}

func Restart(identity daemonctl.Identity, sessionID string) error {
	return runAuthRestart(sessionID, commandDependencies(daemonconn.MayStart(identity)))
}

func Remove(identity daemonctl.Identity, sessionID string) error {
	return runAuthRemove(sessionID, commandDependencies(daemonconn.MayStart(identity)))
}

func runAuthStart(options StartOptions, deps dependencies) error {
	if options.ConfigurationID == "" && options.SessionID == "" {
		if err := input.ValidateNewText(options.Username); err != nil {
			return err
		}
	}
	policy, err := options.Binding.Policy(false)
	if err != nil {
		return err
	}
	if options.Binding.Supplied() && (options.SessionID != "" || options.ConfigurationID != "") {
		return errors.New("网卡绑定选项不能与 --session 或 --config 同时使用")
	}
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		if options.ConfigurationID != "" {
			result, err := callSessionStart(deps, connection, contract.MethodSessionStartConfiguration, contract.ConfigurationIDPayload{ConfigurationID: options.ConfigurationID})
			if err != nil {
				return err
			}
			return terminal.WriteSessionStartResult(deps.Stdout, result)
		}
		if options.SessionID != "" {
			result, err := callSessionStart(deps, connection, contract.MethodSessionEnsureRunning, contract.SessionEnsureRunningPayload{SessionID: options.SessionID})
			if err != nil {
				return err
			}
			return terminal.WriteSessionStartResult(deps.Stdout, result)
		}
		password, err := deps.ValidatedPassword(options.PasswordStdin)
		if err != nil {
			return err
		}
		payload := contract.SessionStartOneShotPayload{
			DisplayName:          options.ProfileID,
			InstitutionProfileID: options.ProfileID,
			Username:             options.Username,
			Password:             password,
			NetworkBindingPolicy: *policy,
		}
		result, err := callSessionStart(deps, connection, contract.MethodSessionStartOneShot, payload)
		if err != nil {
			return err
		}
		return terminal.WriteSessionStartResult(deps.Stdout, result)
	})
}

func runAuthRestart(sessionID string, deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		result, err := callSession(deps, connection, contract.MethodSessionRestart, contract.SessionRestartPayload{SessionID: sessionID})
		if err != nil {
			return err
		}
		return terminal.WriteSessionResult(deps.Stdout, result)
	})
}

func runAuthRemove(sessionID string, deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		rawPayload, err := json.Marshal(contract.SessionRemovePayload{SessionID: sessionID})
		if err != nil {
			return terminal.Failure("编码 Session 请求", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		response, err := connection.Call(ctx, contract.MethodSessionRemove, rawPayload)
		if err != nil {
			return terminal.Failure("调用 Session 操作", err)
		}
		if !response.OK {
			code := ""
			if response.Error != nil {
				code = response.Error.Code
			}
			return errors.New(terminal.IPCErrorText(code))
		}
		result, err := contract.DecodeSessionRemoveResult(response.Result, sessionID)
		if err != nil {
			return terminal.Failure("解码 Session 响应", err)
		}
		return terminal.WriteSessionRemoveResult(deps.Stdout, result)
	})
}

func runAuthStatus(sessionID string, deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		result, err := callSession(
			deps,
			connection,
			contract.MethodSessionGet,
			contract.SessionGetPayload{SessionID: sessionID},
		)
		if err != nil {
			return err
		}
		return terminal.WriteSessionResult(deps.Stdout, result)
	})
}

func runAuthStop(sessionID string, deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		result, err := callSession(
			deps,
			connection,
			contract.MethodSessionStop,
			contract.SessionStopPayload{SessionID: sessionID},
		)
		if err != nil {
			return err
		}
		return terminal.WriteSessionResult(deps.Stdout, result)
	})
}

func callSession(
	deps dependencies,
	connection daemonconn.Client,
	method string,
	payload any,
) (contract.SessionResult, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return contract.SessionResult{}, terminal.Failure("编码 Session 请求", err)
	}

	timeout := deps.connection.Timeout
	if method == contract.MethodSessionEnsureRunning || method == contract.MethodSessionRestart || method == contract.MethodSessionStartConfiguration {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := connection.Call(ctx, method, rawPayload)
	if err != nil {
		return contract.SessionResult{}, terminal.Failure("调用 Session 操作", err)
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		return contract.SessionResult{}, errors.New(terminal.IPCErrorText(code))
	}

	result, err := decodeSessionResult(response.Result)
	if err != nil {
		return contract.SessionResult{}, terminal.Failure("解码 Session 响应", err)
	}
	return result, nil
}

func callSessionStart(deps dependencies, connection daemonconn.Client, method string, payload any) (contract.SessionStartResult, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return contract.SessionStartResult{}, terminal.Failure("编码 Session 请求", err)
	}
	// Call consumes this buffer synchronously; transport copies and strings
	// have separate lifetimes and are outside this cleanup.
	defer clear(rawPayload)
	timeout := deps.connection.Timeout
	if method == contract.MethodSessionEnsureRunning || method == contract.MethodSessionStartConfiguration {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := connection.Call(ctx, method, rawPayload)
	if err != nil {
		return contract.SessionStartResult{}, terminal.Failure("调用 Session 操作", err)
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		return contract.SessionStartResult{}, errors.New(terminal.IPCErrorText(code))
	}
	return decodeSessionStartResult(response.Result)
}

func decodeSessionResult(data []byte) (contract.SessionResult, error) {
	return contract.DecodeSessionResult(data)
}
func decodeSessionStartResult(data []byte) (contract.SessionStartResult, error) {
	return contract.DecodeSessionStartResult(data)
}

func Business(identity daemonctl.Identity, sessionID string) error {
	return runAuthBusiness(sessionID, commandDependencies(daemonconn.Existing(identity)))
}

func runAuthBusiness(sessionID string, deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		payload, err := json.Marshal(contract.SessionGetPayload{SessionID: sessionID})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), deps.connection.Timeout)
		defer cancel()
		response, err := connection.Call(ctx, contract.MethodSessionBusinessGet, payload)
		if err != nil {
			return terminal.Failure("读取业务数据", err)
		}
		if !response.OK {
			code := ""
			if response.Error != nil {
				code = response.Error.Code
			}
			return errors.New(terminal.IPCErrorText(code))
		}
		value, err := contract.DecodeBusinessResult(response.Result)
		if err != nil {
			return terminal.Failure("解码业务数据", err)
		}
		// JSON escapes terminal controls even when a message contains a line break.
		encoder := json.NewEncoder(deps.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	})
}
