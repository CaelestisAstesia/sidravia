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

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

const automaticNetworkBindingPolicy = "automatically_select_latest_available"

type daemonClient interface {
	Call(context.Context, string, json.RawMessage) (contract.Response, error)
	Close() error
}

type daemonConnectionDependencies struct {
	discovery   discoveryDependencies
	connect     func(context.Context, contract.RuntimeInfo) (daemonClient, error)
	callTimeout time.Duration
}

type authDependencies struct {
	connection              daemonConnectionDependencies
	stdin                   io.Reader
	stdout                  io.Writer
	stderr                  io.Writer
	readStdinPassword       func(io.Reader) (string, error)
	readInteractivePassword func(io.Reader, io.Writer) (string, error)
}

func defaultDaemonConnectionDependencies() daemonConnectionDependencies {
	return daemonConnectionDependencies{
		discovery: defaultDiscoveryDependencies(),
		connect: func(ctx context.Context, info contract.RuntimeInfo) (daemonClient, error) {
			return client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
		},
		callTimeout: 2 * time.Second,
	}
}

func defaultAuthDependencies() authDependencies {
	return authDependencies{
		connection:              defaultDaemonConnectionDependencies(),
		stdin:                   os.Stdin,
		stdout:                  os.Stdout,
		stderr:                  os.Stderr,
		readStdinPassword:       readPasswordStdin,
		readInteractivePassword: readInteractivePassword,
	}
}

func authStart(options authStartOptions) error {
	return runAuthStart(options, defaultAuthDependencies())
}

func authStatus(sessionID string) error {
	return runAuthStatus(sessionID, defaultAuthDependencies())
}

func authStop(sessionID string) error {
	return runAuthStop(sessionID, defaultAuthDependencies())
}

func runAuthStart(options authStartOptions, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
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
		result, err := callSession(deps, connection, contract.MethodSessionStartOneShot, payload)
		password = ""
		if err != nil {
			return err
		}
		return writeSessionResult(deps.stdout, result)
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
		return wrapSafeOperation("连接 sidraviad", err)
	}

	operationErr := operation(connection)
	closeErr := connection.Close()
	if operationErr != nil {
		if closeErr != nil {
			return fmt.Errorf("%w; %w", operationErr, wrapSafeOperation("关闭 sidraviad 连接", closeErr))
		}
		return operationErr
	}
	if closeErr != nil {
		return wrapSafeOperation("关闭 sidraviad 连接", closeErr)
	}
	return nil
}

func acquireDaemonClient(deps daemonConnectionDependencies) (daemonClient, error) {
	var acquired daemonClient
	err := discoverDaemon(deps.discovery, func(info contract.RuntimeInfo) error {
		ctx, cancel := context.WithTimeout(context.Background(), deps.callTimeout)
		defer cancel()

		connection, err := deps.connect(ctx, info)
		if err != nil {
			if connection != nil {
				_ = connection.Close()
			}
			return err
		}
		acquired = connection
		return nil
	})
	if err != nil {
		return nil, err
	}
	if acquired == nil {
		return nil, fmt.Errorf("daemon 连接不可用")
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

	ctx, cancel := context.WithTimeout(context.Background(), deps.connection.callTimeout)
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
