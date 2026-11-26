package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/credentialinput"
	"sidravia/internal/ipc/contract"
)

const automaticNetworkBindingPolicy = "automatically_select_latest_available"

type daemonClient = daemonctl.Client

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

type authStartOptions struct {
	binding         bindingFlags
	profileID       string
	username        string
	sessionID       string
	configurationID string
	passwordStdin   bool
}

// defaultDaemonConnectionDependencies delegates all discovery, launch and
// readiness work to bootstrap. Auth owns only operation deadlines and cleanup.
func defaultDaemonConnectionDependencies(identity daemonctl.Identity) daemonConnectionDependencies {
	return daemonConnectionDependencies{
		acquire: func(ctx context.Context) (daemonClient, error) {
			return daemonctl.AcquireHeadless(ctx, identity, "")
		},
		callTimeout: 2 * time.Second,
	}
}

func defaultReadOnlyDaemonConnectionDependencies(identity daemonctl.Identity) daemonConnectionDependencies {
	return daemonConnectionDependencies{
		acquire: func(ctx context.Context) (daemonClient, error) {
			return daemonctl.ConnectExistingHeadless(ctx, identity)
		},
		callTimeout: 2 * time.Second,
	}
}

func defaultAuthDependencies(identity daemonctl.Identity) authDependencies {
	return authDependencies{
		connection:              defaultDaemonConnectionDependencies(identity),
		stdin:                   os.Stdin,
		stdout:                  os.Stdout,
		stderr:                  os.Stderr,
		readStdinPassword:       readPasswordStdin,
		readInteractivePassword: readInteractivePassword,
		inputIsConsole:          isConsoleInput,
	}
}

func authStart(identity daemonctl.Identity, options authStartOptions) error {
	return runAuthStart(options, defaultAuthDependencies(identity))
}

func authStatus(identity daemonctl.Identity, sessionID string) error {
	return runAuthStatus(sessionID, defaultReadOnlyAuthDependencies(identity))
}

func authStop(identity daemonctl.Identity, sessionID string) error {
	return runAuthStop(sessionID, defaultAuthDependencies(identity))
}

func authRestart(identity daemonctl.Identity, sessionID string) error {
	return runAuthRestart(sessionID, defaultAuthDependencies(identity))
}

func authRemove(identity daemonctl.Identity, sessionID string) error {
	return runAuthRemove(sessionID, defaultAuthDependencies(identity))
}

func runAuthStart(options authStartOptions, deps authDependencies) error {
	if options.configurationID == "" && options.sessionID == "" {
		if err := credentialinput.Validate(options.username); err != nil {
			return err
		}
	}
	policy, err := options.binding.policy(false)
	if err != nil {
		return err
	}
	if options.binding.supplied() && (options.sessionID != "" || options.configurationID != "") {
		return errors.New("网卡绑定选项不能与 --session 或 --config 同时使用")
	}
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
		password, err := deps.readValidatedPassword(options.passwordStdin)
		if err != nil {
			return err
		}
		payload := contract.SessionStartOneShotPayload{
			DisplayName:          options.profileID,
			InstitutionProfileID: options.profileID,
			Username:             options.username,
			Password:             password,
			NetworkBindingPolicy: *policy,
		}
		result, err := callSessionStart(deps, connection, contract.MethodSessionStartOneShot, payload)
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
		result, err := contract.DecodeSessionRemoveResult(response.Result, sessionID)
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
		if daemonctl.IsReadinessUnconfirmed(err) {
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
	// Call consumes this buffer synchronously; transport copies and strings
	// have separate lifetimes and are outside this cleanup.
	defer clear(rawPayload)
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

func writeSessionRemoveResult(output io.Writer, result contract.SessionRemoveResult) error {
	p := newPresentation(output)
	return wrapSafeOperation("写入 Session 删除响应", p.complete(renderSessionRemoved(&result)))
}

func decodeSessionResult(data []byte) (contract.SessionResult, error) {
	return contract.DecodeSessionResult(data)
}
func decodeSessionStartResult(data []byte) (contract.SessionStartResult, error) {
	return contract.DecodeSessionStartResult(data)
}
func validateSessionResult(value contract.SessionResult) error {
	_, err := contract.MarshalSessionResult(value)
	return err
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

// bindingFlags retains flag presence so an explicit empty value is invalid.
type bindingFlags struct {
	interfaceID, localIPv4              string
	interfaceSet, addressSet, automatic bool
}

func (flags bindingFlags) supplied() bool {
	return flags.interfaceSet || flags.addressSet || flags.automatic
}
func (flags bindingFlags) policy(preserve bool) (*contract.NetworkBindingPolicy, error) {
	if flags.automatic && (flags.interfaceSet || flags.addressSet) {
		return nil, errors.New("--automatic-binding 不能与指定网卡或 IPv4 同时使用")
	}
	if flags.interfaceSet != flags.addressSet {
		return nil, errors.New("--interface-id 与 --local-ipv4 必须同时提供")
	}
	if !flags.supplied() && preserve {
		return nil, nil
	}
	policy := contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy}
	if flags.interfaceSet {
		policy = contract.NetworkBindingPolicy{Mode: "explicit_interface_and_local_ipv4", InterfaceID: flags.interfaceID, LocalIPv4Address: flags.localIPv4}
	}
	if _, err := policy.Domain(); err != nil {
		return nil, errors.New("指定网卡 ID 或本地 IPv4 地址无效")
	}
	return &policy, nil
}

func runAuthBusiness(sessionID string, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		payload, err := json.Marshal(contract.SessionGetPayload{SessionID: sessionID})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), deps.connection.callTimeout)
		defer cancel()
		response, err := connection.Call(ctx, contract.MethodSessionBusinessGet, payload)
		if err != nil {
			return wrapSafeOperation("读取业务数据", err)
		}
		if !response.OK {
			code := ""
			if response.Error != nil {
				code = response.Error.Code
			}
			return errors.New(ipcErrorText(code))
		}
		value, err := contract.DecodeBusinessResult(response.Result)
		if err != nil {
			return wrapSafeOperation("解码业务数据", err)
		}
		// JSON escapes terminal controls even when a message contains a line break.
		encoder := json.NewEncoder(deps.stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	})
}
