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

const automaticNetworkBindingPolicy = "automatically_select_latest_available"

type daemonClient = clientbootstrap.Client

type daemonConnectionDependencies struct {
	acquire     func(context.Context) (daemonClient, error)
	callTimeout time.Duration
}

type authDependencies struct {
	connection              daemonConnectionDependencies
	stdin                   io.Reader
	stdout                  io.Writer
	stderr                  io.Writer
	readStdinPassword       func(io.Reader) (string, error)
	readInteractivePassword func(io.Reader, io.Writer) (string, error)
	inputIsConsole          func(io.Reader) bool
}

// defaultDaemonConnectionDependencies delegates all discovery, launch and
// readiness work to bootstrap. Auth owns only operation deadlines and cleanup.
func defaultDaemonConnectionDependencies(identity clientbootstrap.Identity) daemonConnectionDependencies {
	return daemonConnectionDependencies{
		acquire: func(ctx context.Context) (daemonClient, error) {
			return clientbootstrap.AcquireHeadless(ctx, identity, "")
		},
		callTimeout: 2 * time.Second,
	}
}

func defaultReadOnlyDaemonConnectionDependencies(identity clientbootstrap.Identity) daemonConnectionDependencies {
	return daemonConnectionDependencies{
		acquire: func(ctx context.Context) (daemonClient, error) {
			return clientbootstrap.ConnectExistingHeadless(ctx, identity)
		},
		callTimeout: 2 * time.Second,
	}
}

func defaultAuthDependencies(identity clientbootstrap.Identity) authDependencies {
	return authDependencies{
		connection:              defaultDaemonConnectionDependencies(identity),
		stdin:                   os.Stdin,
		stdout:                  os.Stdout,
		stderr:                  os.Stderr,
		readStdinPassword:       readPasswordStdin,
		readInteractivePassword: readInteractivePassword,
		inputIsConsole: func(input io.Reader) bool {
			file, ok := input.(*os.File)
			if !ok {
				return false
			}
			info, err := file.Stat()
			return err == nil && info.Mode()&os.ModeCharDevice != 0
		},
	}
}

func authStart(identity clientbootstrap.Identity, options authStartOptions) error {
	return runAuthStart(options, defaultAuthDependencies(identity))
}

func authStatus(identity clientbootstrap.Identity, sessionID string) error {
	return runAuthStatus(sessionID, defaultReadOnlyAuthDependencies(identity))
}

func authStop(identity clientbootstrap.Identity, sessionID string) error {
	return runAuthStop(sessionID, defaultAuthDependencies(identity))
}

func authRestart(identity clientbootstrap.Identity, sessionID string) error {
	return runAuthRestart(sessionID, defaultAuthDependencies(identity))
}

func authRemove(identity clientbootstrap.Identity, sessionID string) error {
	return runAuthRemove(sessionID, defaultAuthDependencies(identity))
}

func runAuthStart(options authStartOptions, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		if options.configurationID != "" {
			result, err := callSessionStart(deps, connection, contract.MethodSessionStartConfiguration, contract.ConfigurationIDPayload{ConfigurationID: options.configurationID})
			if err != nil {
				return err
			}
			return writeSessionStartResult(deps.stdout, result)
		}
		if options.sessionID != "" {
			result, err := callSessionStart(deps, connection, contract.MethodSessionEnsureRunning, contract.SessionEnsureRunningPayload{SessionID: options.sessionID})
			if err != nil {
				return err
			}
			return writeSessionStartResult(deps.stdout, result)
		}
		var password string
		var err error
		if options.passwordStdin {
			password, err = deps.readStdinPassword(deps.stdin)
		} else {
			password, err = deps.readInteractivePassword(deps.stdin, deps.stderr)
		}
		if err != nil {
			return err
		}

		payload := contract.SessionStartOneShotPayload{
			DisplayName:              options.profileID,
			InstitutionProfileID:     options.profileID,
			Username:                 options.username,
			Password:                 password,
			NetworkBindingPolicyMode: automaticNetworkBindingPolicy,
			ProtocolContextOverride:  json.RawMessage("{}"),
		}
		result, err := callSessionStart(deps, connection, contract.MethodSessionStartOneShot, payload)
		password = ""
		if err != nil {
			return err
		}
		return writeSessionStartResult(deps.stdout, result)
	})
}

func runAuthRestart(sessionID string, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		result, err := callSession(deps, connection, contract.MethodSessionRestart, contract.SessionRestartPayload{SessionID: sessionID})
		if err != nil {
			return err
		}
		return writeSessionResult(deps.stdout, result)
	})
}

func runAuthRemove(sessionID string, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		rawPayload, err := json.Marshal(contract.SessionRemovePayload{SessionID: sessionID})
		if err != nil {
			return wrapSafeOperation("编码 Session 请求", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		response, err := connection.Call(ctx, contract.MethodSessionRemove, rawPayload)
		if err != nil {
			return wrapSafeOperation("调用 Session 操作", err)
		}
		if !response.OK {
			code := ""
			if response.Error != nil {
				code = response.Error.Code
			}
			return errors.New(ipcErrorText(code))
		}
		result, err := decodeSessionRemoveResult(response.Result)
		if err != nil {
			return wrapSafeOperation("解码 Session 响应", err)
		}
		return writeSessionRemoveResult(deps.stdout, result)
	})
}

func runAuthStatus(sessionID string, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		result, err := callSession(
			deps,
			connection,
			contract.MethodSessionGet,
			contract.SessionGetPayload{SessionID: sessionID},
		)
		if err != nil {
			return err
		}
		return writeSessionResult(deps.stdout, result)
	})
}

func runAuthStop(sessionID string, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		result, err := callSession(
			deps,
			connection,
			contract.MethodSessionStop,
			contract.SessionStopPayload{SessionID: sessionID},
		)
		if err != nil {
			return err
		}
		return writeSessionResult(deps.stdout, result)
	})
}

func withAuthClient(deps authDependencies, operation func(daemonClient) error) error {
	return withDaemonClient(deps.connection, operation)
}

func withDaemonClient(deps daemonConnectionDependencies, operation func(daemonClient) error) error {
	connection, err := acquireDaemonClient(deps)
	if err != nil {
		if clientbootstrap.IsReadinessUnconfirmed(err) {
			return err
		}
		return wrapSafeOperation("无法连接 sidraviad（请确认 daemon 已启动；若刚增删过 sidravia.portable 标记，请先停止并重启 daemon）", err)
	}

	operationErr := operation(connection)
	closeErr := connection.Close()
	if operationErr != nil {
		if closeErr != nil {
			return fmt.Errorf("%w; %w", operationErr, wrapSafeOperation("清理 sidraviad 连接", closeErr))
		}
		return operationErr
	}
	// The operation succeeded; a connection cleanup failure must not turn a
	// successful result into a user-visible error.
	_ = closeErr
	return nil
}

func acquireDaemonClient(deps daemonConnectionDependencies) (daemonClient, error) {
	acquired, err := deps.acquire(context.Background())
	if err != nil {
		return nil, err
	}
	if acquired == nil {
		return nil, fmt.Errorf("无法连接 daemon（未找到运行中的 daemon）")
	}
	return acquired, nil
}

func callSession(
	deps authDependencies,
	connection daemonClient,
	method string,
	payload any,
) (contract.SessionResult, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return contract.SessionResult{}, wrapSafeOperation("编码 Session 请求", err)
	}

	timeout := deps.connection.callTimeout
	if method == contract.MethodSessionEnsureRunning || method == contract.MethodSessionRestart || method == contract.MethodSessionStartConfiguration {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := connection.Call(ctx, method, rawPayload)
	if err != nil {
		return contract.SessionResult{}, wrapSafeOperation("调用 Session 操作", err)
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		return contract.SessionResult{}, errors.New(ipcErrorText(code))
	}

	result, err := decodeSessionResult(response.Result)
	if err != nil {
		return contract.SessionResult{}, wrapSafeOperation("解码 Session 响应", err)
	}
	return result, nil
}

func callSessionStart(deps authDependencies, connection daemonClient, method string, payload any) (contract.SessionStartResult, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return contract.SessionStartResult{}, wrapSafeOperation("编码 Session 请求", err)
	}
	timeout := deps.connection.callTimeout
	if method == contract.MethodSessionEnsureRunning || method == contract.MethodSessionStartConfiguration {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := connection.Call(ctx, method, rawPayload)
	if err != nil {
		return contract.SessionStartResult{}, wrapSafeOperation("调用 Session 操作", err)
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		return contract.SessionStartResult{}, errors.New(ipcErrorText(code))
	}
	return decodeSessionStartResult(response.Result)
}

func decodeSessionRemoveResult(data []byte) (contract.SessionRemoveResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result contract.SessionRemoveResult
	if err := decoder.Decode(&result); err != nil {
		return contract.SessionRemoveResult{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return contract.SessionRemoveResult{}, fmt.Errorf("Session 删除结果之后存在多余数据")
	}
	if result.SessionID == "" || result.Status != "removed" {
		return contract.SessionRemoveResult{}, fmt.Errorf("Session 删除结果无效")
	}
	return result, nil
}

func writeSessionRemoveResult(output io.Writer, result contract.SessionRemoveResult) error {
	p := newPresentation(output)
	return wrapSafeOperation("写入 Session 删除响应", p.complete(renderSessionRemoved(&result)))
}

func decodeSessionResult(data []byte) (contract.SessionResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var result contract.SessionResult
	if err := decoder.Decode(&result); err != nil {
		return contract.SessionResult{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return contract.SessionResult{}, fmt.Errorf("Session 结果之后存在多余数据")
		}
		return contract.SessionResult{}, fmt.Errorf("Session 结果之后存在多余数据")
	}
	if err := validateSessionResult(result); err != nil {
		return contract.SessionResult{}, err
	}
	return result, nil
}

func decodeSessionStartResult(data []byte) (contract.SessionStartResult, error) {
	var result contract.SessionStartResult
	if err := decodeStrictCLI(data, &result); err != nil {
		return result, err
	}
	if result.Outcome != "created" && result.Outcome != "already_running" && result.Outcome != "resumed" {
		return result, fmt.Errorf("Session 启动结果无效")
	}
	if err := validateSessionResult(result.Session); err != nil {
		return result, err
	}
	return result, nil
}

func validateSessionResult(result contract.SessionResult) error {
	switch {
	case result.AuthenticationSessionID == "":
		return fmt.Errorf("Session 结果缺少 SessionID")
	case result.State == "":
		return fmt.Errorf("Session 结果缺少状态")
	case result.InstitutionProfileID == "":
		return fmt.Errorf("Session 结果缺少 Profile ID")
	case result.AuthenticationProtocolID == "":
		return fmt.Errorf("Session 结果缺少协议 ID")
	case result.AccountName == "":
		return fmt.Errorf("Session 结果缺少账号名称")
	case result.UpdatedAt == "":
		return fmt.Errorf("Session 结果缺少更新时间")
	default:
		return nil
	}
}

func writeSessionResult(output io.Writer, result contract.SessionResult) error {
	if err := validateSessionResult(result); err != nil {
		return wrapSafeOperation("渲染 Session 响应", err)
	}
	p := newPresentation(output)
	return wrapSafeOperation("写入 Session 响应", p.complete(renderSessionDetail(p, &result)))
}

func writeSessionStartResult(output io.Writer, result contract.SessionStartResult) error {
	if err := validateSessionResult(result.Session); err != nil {
		return wrapSafeOperation("渲染 Session 启动响应", err)
	}
	p := newPresentation(output)
	return wrapSafeOperation("写入 Session 启动响应", p.complete(renderSessionStartResult(p, &result)))
}
