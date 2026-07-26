package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

type authDependencies struct {
	discovery               discoveryDependencies
	connect                 func(context.Context, contract.RuntimeInfo) (daemonClient, error)
	callTimeout             time.Duration
	stdin                   io.Reader
	stdout                  io.Writer
	stderr                  io.Writer
	readStdinPassword       func(io.Reader) (string, error)
	readInteractivePassword func(io.Reader, io.Writer) (string, error)
}

func defaultAuthDependencies() authDependencies {
	return authDependencies{
		discovery: defaultDiscoveryDependencies(),
		connect: func(ctx context.Context, info contract.RuntimeInfo) (daemonClient, error) {
			return client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
		},
		callTimeout:             2 * time.Second,
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
	connection, err := acquireAuthClient(deps)
	if err != nil {
		return wrapSafeOperation("connect to sidraviad", err)
	}

	operationErr := operation(connection)
	closeErr := connection.Close()
	if operationErr != nil {
		if closeErr != nil {
			return fmt.Errorf("%w; %w", operationErr, wrapSafeOperation("close sidraviad connection", closeErr))
		}
		return operationErr
	}
	if closeErr != nil {
		return wrapSafeOperation("close sidraviad connection", closeErr)
	}
	return nil
}

func acquireAuthClient(deps authDependencies) (daemonClient, error) {
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
		return nil, fmt.Errorf("daemon connection unavailable")
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
		return contract.SessionResult{}, wrapSafeOperation("encode Session request", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), deps.callTimeout)
	defer cancel()
	response, err := connection.Call(ctx, method, rawPayload)
	if err != nil {
		return contract.SessionResult{}, wrapSafeOperation("call Session operation", err)
	}
	if !response.OK {
		if response.Error == nil {
			return contract.SessionResult{}, fmt.Errorf("daemon returned malformed Session error")
		}
		return contract.SessionResult{}, fmt.Errorf(
			"daemon Session error %s: %s",
			response.Error.Code,
			response.Error.Message,
		)
	}

	result, err := decodeSessionResult(response.Result)
	if err != nil {
		return contract.SessionResult{}, wrapSafeOperation("decode Session response", err)
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
			return contract.SessionResult{}, fmt.Errorf("trailing Session result")
		}
		return contract.SessionResult{}, fmt.Errorf("trailing Session result data")
	}
	if err := validateSessionResult(result); err != nil {
		return contract.SessionResult{}, err
	}
	return result, nil
}

func validateSessionResult(result contract.SessionResult) error {
	switch {
	case result.AuthenticationSessionID == "":
		return fmt.Errorf("Session result is missing SessionID")
	case result.State == "":
		return fmt.Errorf("Session result is missing state")
	case result.InstitutionProfileID == "":
		return fmt.Errorf("Session result is missing Profile ID")
	case result.AuthenticationProtocolID == "":
		return fmt.Errorf("Session result is missing protocol ID")
	case result.AccountLabel == "":
		return fmt.Errorf("Session result is missing account label")
	case result.UpdatedAt == "":
		return fmt.Errorf("Session result is missing updated timestamp")
	default:
		return nil
	}
}

func writeSessionResult(output io.Writer, result contract.SessionResult) error {
	if err := validateSessionResult(result); err != nil {
		return wrapSafeOperation("render Session response", err)
	}

	var block bytes.Buffer
	fmt.Fprintf(&block, "Session: %s\n", result.AuthenticationSessionID)
	fmt.Fprintf(&block, "State: %s\n", result.State)
	if result.InstitutionDisplayName == "" {
		fmt.Fprintf(&block, "Profile: %s\n", result.InstitutionProfileID)
	} else {
		fmt.Fprintf(
			&block,
			"Profile: %s (%s)\n",
			result.InstitutionDisplayName,
			result.InstitutionProfileID,
		)
	}
	fmt.Fprintf(&block, "Protocol: %s\n", result.AuthenticationProtocolID)
	fmt.Fprintf(&block, "Account: %s\n", result.AccountLabel)
	if result.StateReason != nil {
		fmt.Fprintf(
			&block,
			"Reason: %s — %s\n",
			result.StateReason.Code,
			result.StateReason.Description,
		)
	}
	if result.SelectedNetworkBinding != nil {
		fmt.Fprintf(
			&block,
			"Network: %s [%s] — %s\n",
			result.SelectedNetworkBinding.DisplayName,
			result.SelectedNetworkBinding.InterfaceID,
			result.SelectedNetworkBinding.LocalIPv4Address,
		)
	}
	if result.AuthenticationEstablishedAt != nil {
		fmt.Fprintf(&block, "Authenticated: %s\n", *result.AuthenticationEstablishedAt)
	}
	if result.NextRetryAt != nil {
		fmt.Fprintf(&block, "Retry: %s\n", *result.NextRetryAt)
	}
	if result.LastAuthenticationFailure != nil {
		fmt.Fprintf(
			&block,
			"Failure: %s — %s (%s)\n",
			result.LastAuthenticationFailure.Code,
			result.LastAuthenticationFailure.Description,
			result.LastAuthenticationFailure.HandlingRecommendation,
		)
	}
	fmt.Fprintf(&block, "Updated: %s\n", result.UpdatedAt)

	if err := writeAll(output, block.String()); err != nil {
		return wrapSafeOperation("write Session response", err)
	}
	return nil
}
