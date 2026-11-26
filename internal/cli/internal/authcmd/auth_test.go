package authcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"sidravia/internal/cli/internal/bindingargs"
	"sidravia/internal/cli/internal/clitest"
	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

func TestSessionStartClearsOwnedWireBytesOnEveryExit(t *testing.T) {
	cause := errors.New("fictional transport failure")
	for _, outcome := range []string{"success", "transport", "typed error", "decode error"} {
		t.Run(outcome, func(t *testing.T) {
			var retained json.RawMessage
			connection := &clitest.FakeDaemonClient{CallFunc: func(method string, buffer json.RawMessage) (contract.Response, error) {
				retained = buffer
				value, err := contract.DecodeSessionStartOneShotPayload(buffer)
				if method != contract.MethodSessionStartOneShot || err != nil || value.Password != "fictional" {
					t.Fatal("wire payload changed before Call consumed it")
				}
				switch outcome {
				case "transport":
					return contract.Response{}, cause
				case "typed error":
					return contract.NewErrorResponse("1", contract.ErrorCodeInvalidArgument, "invalid"), nil
				case "decode error":
					return contract.NewSuccessResponse("1", json.RawMessage(`invalid`)), nil
				default:
					return clitest.SuccessSessionStartResponse(t, "created", clitest.MinimalSessionResult("authenticated")), nil
				}
			}}
			deps := hotAuthDependencies(t, connection)
			_, err := callSessionStart(deps, connection, contract.MethodSessionStartOneShot, contract.SessionStartOneShotPayload{
				InstitutionProfileID: "jlu", Username: "user", Password: "fictional",
				NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: "automatically_select_latest_available"},
			})
			if (err == nil) != (outcome == "success") || (outcome == "transport" && !errors.Is(err, cause)) {
				t.Fatal("operation error behavior changed")
			}
			clitest.AssertClearedBytes(t, retained)
		})
	}
}

func TestRunAuthStartRetainedDoesNotReadPassword(t *testing.T) {
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionEnsureRunning || string(payload) != `{"sessionId":"session-1"}` {
			t.Fatalf("call = %s %s", method, payload)
		}
		return clitest.SuccessSessionStartResponse(t, "already_running", clitest.MinimalSessionResult("authenticated")), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.ReadStdinPassword = func(io.Reader) (string, error) { t.Fatal("stdin password read"); return "", nil }
	deps.ReadInteractivePassword = func(io.Reader, io.Writer) (string, error) { t.Fatal("interactive password read"); return "", nil }
	if err := runAuthStart(StartOptions{SessionID: "session-1"}, deps); err != nil {
		t.Fatal(err)
	}
}

func TestRunAuthStartRetainedRejectsRetiredBareSessionResult(t *testing.T) {
	var output bytes.Buffer
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionEnsureRunning {
			t.Fatalf("method = %q", method)
		}
		return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult("authenticated")), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.Stdout = &output
	if err := runAuthStart(StartOptions{SessionID: "session-1"}, deps); err == nil {
		t.Fatal("retired bare result succeeded")
	}
	if output.Len() != 0 {
		t.Fatalf("retired bare result produced success output: %q", output.String())
	}
}

func TestRunAuthStartConfigurationTypedRequestDeadlinePresentationAndNoPasswordRead(t *testing.T) {
	var output bytes.Buffer
	connection := &clitest.FakeDaemonClient{
		CallContext: func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("configuration start has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining < 29*time.Second || remaining > 31*time.Second {
				t.Fatalf("configuration start deadline = %v", remaining)
			}
		},
		CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionStartConfiguration ||
				string(payload) != `{"configurationId":"campus"}` {
				t.Fatalf("call = %s %s", method, payload)
			}
			result := clitest.MinimalSessionResult("authenticated")
			result.AuthenticationSessionID = "configuration-session"
			result.InstitutionDisplayName = "Campus"
			return clitest.SuccessSessionStartResponse(t, "created", result), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	deps.Stdout = &output
	deps.ReadStdinPassword = func(io.Reader) (string, error) {
		t.Fatal("config start read stdin password")
		return "", nil
	}
	deps.ReadInteractivePassword = func(io.Reader, io.Writer) (string, error) {
		t.Fatal("config start read interactive password")
		return "", nil
	}
	if err := runAuthStart(StartOptions{ConfigurationID: "campus"}, deps); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"会话：configuration-session", "状态：已认证", "机构：Campus"} {
		if !strings.Contains(output.String(), text) {
			t.Fatalf("presentation %q missing %q", output.String(), text)
		}
	}
}

func TestRetainedLifecycleDispatchPayloadAndDeadline(t *testing.T) {
	tests := []struct {
		name   string
		method string
		run    func(string, dependencies) error
		result func(t *testing.T) contract.Response
	}{
		{"ensure", contract.MethodSessionEnsureRunning, func(id string, deps dependencies) error {
			return runAuthStart(StartOptions{SessionID: id}, deps)
		}, func(t *testing.T) contract.Response {
			return clitest.SuccessSessionStartResponse(t, "already_running", clitest.MinimalSessionResult("authenticated"))
		}},
		{"restart", contract.MethodSessionRestart, runAuthRestart, func(t *testing.T) contract.Response {
			return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult("authenticating"))
		}},
		{"remove", contract.MethodSessionRemove, runAuthRemove, func(t *testing.T) contract.Response {
			return contract.NewSuccessResponse("1", json.RawMessage(`{"sessionId":"session-1","status":"removed"}`))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
				if method != test.method || string(payload) != `{"sessionId":"session-1"}` {
					t.Fatalf("call=%s %s", method, payload)
				}
				return test.result(t), nil
			}}
			connection.CallContext = func(ctx context.Context) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("operation context has no deadline")
				}
				remaining := time.Until(deadline)
				if remaining < 29*time.Second || remaining > 31*time.Second {
					t.Fatalf("operation deadline remaining=%v", remaining)
				}
			}
			deps := hotAuthDependencies(t, connection)
			if err := test.run("session-1", deps); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunAuthRemoveRejectsInvalidReceipts(t *testing.T) {
	for _, data := range []string{
		`{"sessionId":"","status":"removed"}`,
		`{"sessionId":"session-1","status":"wrong"}`,
		`{"sessionId":"session-2","status":"removed"}`,
		`{"status":"removed"}`,
		`{"sessionId":"session-1","status":"removed","status":"removed"}`,
		`{"sessionId":"session-1","status":"removed","extra":true}`,
		`{"sessionId":"session-1","status":"removed"}{}`,
	} {
		connection := &clitest.FakeDaemonClient{CallFunc: func(method string, _ json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionRemove {
				t.Fatalf("unexpected method %s", method)
			}
			return contract.NewSuccessResponse("1", json.RawMessage(data)), nil
		}}
		deps := hotAuthDependencies(t, connection)
		var output bytes.Buffer
		deps.Stdout = &output
		if err := runAuthRemove("session-1", deps); err == nil || err.Error() != "解码 Session 响应" || output.Len() != 0 {
			t.Fatalf("invalid remove receipt %s produced output %q, error %v", data, &output, err)
		}
	}
}

func TestRunAuthRemovePreservesTransportAndWriteCauses(t *testing.T) {
	transportCause := errors.New("transport-cause")
	connection := &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
		return contract.Response{}, transportCause
	}}
	if err := runAuthRemove("session-1", hotAuthDependencies(t, connection)); !errors.Is(err, transportCause) {
		t.Fatalf("transport cause not preserved: %v", err)
	}

	writeCause := errors.New("write-cause")
	connection = &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewSuccessResponse("1", json.RawMessage(`{"sessionId":"session-1","status":"removed"}`)), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.Stdout = clitest.ZeroWriter{Err: writeCause}
	if err := runAuthRemove("session-1", deps); !errors.Is(err, writeCause) {
		t.Fatalf("write cause not preserved: %v", err)
	}

	const publicMarker = "daemon-public-marker"
	const wrappedMarker = "daemon-wrapped-marker"
	connection = &clitest.FakeDaemonClient{CallFunc: func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewErrorResponse("1", contract.ErrorCodeSessionOperationFailed, publicMarker), errors.New(wrappedMarker)
	}}
	var output bytes.Buffer
	deps = hotAuthDependencies(t, connection)
	deps.Stdout = &output
	err := runAuthRemove("session-1", deps)
	if err == nil {
		t.Fatal("daemon error returned nil")
	}
	if strings.Contains(err.Error(), publicMarker) || strings.Contains(err.Error(), wrappedMarker) ||
		strings.Contains(output.String(), publicMarker) || strings.Contains(output.String(), wrappedMarker) {
		t.Fatalf("daemon failure leaked markers: err=%q output=%q", err, output.String())
	}
}

func hotAuthDependencies(t *testing.T, connection daemonconn.Client) dependencies {
	t.Helper()
	return dependencies{
		connection: daemonconn.Connection{
			Acquire: func(context.Context) (daemonconn.Client, error) {
				return connection, nil
			},
			Timeout: time.Second,
		},
		IO: clitest.PromptIO(t),
	}
}

func TestAuthDiscoveryReturnsHotClientAndClosesIt(t *testing.T) {
	connection := &clitest.FakeDaemonClient{
		CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionGet {
				t.Errorf("method = %q, want session.get", method)
			}
			return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult("authenticated")), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	acquires := 0
	deps.connection.Acquire = func(context.Context) (daemonconn.Client, error) {
		acquires++
		return connection, nil
	}

	if err := runAuthStatus("session-1", deps); err != nil {
		t.Fatalf("runAuthStatus = %v, want nil", err)
	}
	if acquires != 1 {
		t.Errorf("acquire calls = %d, want 1", acquires)
	}
	if connection.CallCount != 1 {
		t.Errorf("Session calls = %d, want 1", connection.CallCount)
	}
	if connection.CloseCount != 1 {
		t.Errorf("Close calls = %d, want 1", connection.CloseCount)
	}
}

func TestAuthBootstrapFailureDoesNotDispatchSession(t *testing.T) {
	freshClient := &clitest.FakeDaemonClient{
		CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionStop {
				t.Errorf("method = %q, want session.stop", method)
			}
			decoded, err := contract.DecodeSessionStopPayload(payload)
			if err != nil {
				t.Fatalf("DecodeSessionStopPayload = %v", err)
			}
			if decoded.SessionID != "session-1" {
				t.Error("fresh client received different SessionID")
			}
			return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult("stopping")), nil
		},
	}

	deps := hotAuthDependencies(t, freshClient)
	deferred := true
	deps.connection.Acquire = func(context.Context) (daemonconn.Client, error) {
		if deferred {
			deferred = false
			return nil, errors.New("bootstrap unavailable")
		}
		return freshClient, nil
	}
	err := runAuthStop("session-1", deps)
	if err == nil || !strings.Contains(err.Error(), "无法连接 sidraviad") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if freshClient.CallCount != 0 {
		t.Fatalf("session call count=%d", freshClient.CallCount)
	}
}

func TestAuthStartInteractiveTypedRequestAndSecrecy(t *testing.T) {
	username := "username-secret-marker"
	password := "password-secret-marker"
	var method string
	var rawPayload json.RawMessage
	connection := &clitest.FakeDaemonClient{
		CallFunc: func(gotMethod string, gotPayload json.RawMessage) (contract.Response, error) {
			method = gotMethod
			rawPayload = append(json.RawMessage(nil), gotPayload...)
			return clitest.SuccessSessionStartResponse(t, "created", clitest.MinimalSessionResult("authenticating")), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	var stdout, stderr bytes.Buffer
	deps.Stdout = &stdout
	deps.Stderr = &stderr
	interactiveReads, stdinReads := 0, 0
	deps.ReadInteractivePassword = func(io.Reader, io.Writer) (string, error) {
		interactiveReads++
		return password, nil
	}
	deps.ReadStdinPassword = func(io.Reader) (string, error) {
		stdinReads++
		return "", nil
	}

	err := runAuthStart(StartOptions{
		ProfileID: "profile-1",
		Username:  username,
	}, deps)
	if err != nil {
		t.Fatalf("runAuthStart = %v, want nil", err)
	}
	if method != contract.MethodSessionStartOneShot {
		t.Errorf("method = %q, want session.startOneShot", method)
	}
	if connection.CallCount != 1 {
		t.Errorf("Call count = %d, want 1", connection.CallCount)
	}
	if connection.CloseCount != 1 {
		t.Errorf("Close count = %d, want 1", connection.CloseCount)
	}
	if interactiveReads != 1 || stdinReads != 0 {
		t.Errorf("password reader calls = interactive %d, stdin %d; want 1, 0", interactiveReads, stdinReads)
	}

	decoded, decodeErr := contract.DecodeSessionStartOneShotPayload(rawPayload)
	if decodeErr != nil {
		t.Fatalf("DecodeSessionStartOneShotPayload = %v", decodeErr)
	}
	if decoded.DisplayName != "profile-1" ||
		decoded.InstitutionProfileID != "profile-1" ||
		decoded.NetworkBindingPolicy.Mode != "automatically_select_latest_available" {
		t.Error("start payload non-secret fields differ from accepted values")
	}
	if decoded.Username != username || decoded.Password != password {
		t.Error("start payload credential fields differ from accepted input")
	}
	for _, visible := range []string{stdout.String(), stderr.String(), clitest.ErrorString(err)} {
		if strings.Contains(visible, username) || strings.Contains(visible, password) {
			t.Error("interactive start exposed a credential marker")
		}
	}
}

func TestAuthStartStdinTypedRequestAndSecrecy(t *testing.T) {
	username := "stdin-username-marker"
	password := "stdin-password-marker"
	var rawPayload json.RawMessage
	connection := &clitest.FakeDaemonClient{
		CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionStartOneShot {
				t.Errorf("method = %q, want session.startOneShot", method)
			}
			rawPayload = append(json.RawMessage(nil), payload...)
			return clitest.SuccessSessionStartResponse(t, "created", clitest.MinimalSessionResult("waiting_for_network")), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	var stdout, stderr bytes.Buffer
	deps.Stdout = &stdout
	deps.Stderr = &stderr
	stdinReads, interactiveReads := 0, 0
	deps.ReadStdinPassword = func(io.Reader) (string, error) {
		stdinReads++
		return password, nil
	}
	deps.ReadInteractivePassword = func(io.Reader, io.Writer) (string, error) {
		interactiveReads++
		return "", nil
	}

	err := runAuthStart(StartOptions{
		ProfileID:     "profile-1",
		Username:      username,
		PasswordStdin: true,
	}, deps)
	if err != nil {
		t.Fatalf("runAuthStart = %v, want nil", err)
	}
	if connection.CallCount != 1 {
		t.Errorf("Call count = %d, want 1", connection.CallCount)
	}
	if stdinReads != 1 || interactiveReads != 0 {
		t.Errorf("password reader calls = stdin %d, interactive %d; want 1, 0", stdinReads, interactiveReads)
	}

	decoded, decodeErr := contract.DecodeSessionStartOneShotPayload(rawPayload)
	if decodeErr != nil {
		t.Fatalf("DecodeSessionStartOneShotPayload = %v", decodeErr)
	}
	if decoded.DisplayName != "profile-1" ||
		decoded.InstitutionProfileID != "profile-1" ||
		decoded.Username != username ||
		decoded.Password != password ||
		decoded.NetworkBindingPolicy.Mode != "automatically_select_latest_available" {
		t.Error("stdin start payload differs from accepted input")
	}
	for _, visible := range []string{stdout.String(), stderr.String(), clitest.ErrorString(err)} {
		if strings.Contains(visible, username) || strings.Contains(visible, password) {
			t.Error("stdin start exposed a credential marker")
		}
	}
}

func TestAuthStatusAndStopTypedRequestsWithoutPasswordRead(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		run         func(string, dependencies) error
		decodeID    func([]byte) (string, error)
		resultState string
	}{
		{
			name:   "status",
			method: contract.MethodSessionGet,
			run:    runAuthStatus,
			decodeID: func(payload []byte) (string, error) {
				decoded, err := contract.DecodeSessionGetPayload(payload)
				return decoded.SessionID, err
			},
			resultState: "authenticated",
		},
		{
			name:   "stop",
			method: contract.MethodSessionStop,
			run:    runAuthStop,
			decodeID: func(payload []byte) (string, error) {
				decoded, err := contract.DecodeSessionStopPayload(payload)
				return decoded.SessionID, err
			},
			resultState: "stopping",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var rawPayload json.RawMessage
			connection := &clitest.FakeDaemonClient{
				CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
					if method != test.method {
						t.Errorf("method = %q, want %q", method, test.method)
					}
					rawPayload = append(json.RawMessage(nil), payload...)
					return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult(test.resultState)), nil
				},
			}
			deps := hotAuthDependencies(t, connection)
			passwordReads := 0
			deps.ReadStdinPassword = func(io.Reader) (string, error) {
				passwordReads++
				return "", nil
			}
			deps.ReadInteractivePassword = func(io.Reader, io.Writer) (string, error) {
				passwordReads++
				return "", nil
			}

			if err := test.run("session-request-id", deps); err != nil {
				t.Fatalf("Session command = %v, want nil", err)
			}
			sessionID, err := test.decodeID(rawPayload)
			if err != nil {
				t.Fatalf("strict payload decode = %v", err)
			}
			if sessionID != "session-request-id" {
				t.Error("typed payload contains different SessionID")
			}
			if connection.CallCount != 1 {
				t.Errorf("Call count = %d, want 1", connection.CallCount)
			}
			if connection.CloseCount != 1 {
				t.Errorf("Close count = %d, want 1", connection.CloseCount)
			}
			if passwordReads != 0 {
				t.Errorf("password reads = %d, want 0", passwordReads)
			}
		})
	}
}

func TestWriteSessionResultCompleteOutput(t *testing.T) {
	authenticatedAt := "2026-07-26T10:11:12.123456789+08:00"
	retryAt := "2026-07-26T10:11:17.123456789+08:00"
	result := clitest.MinimalSessionResult("waiting_before_retry")
	result.InstitutionDisplayName = "Example University"
	result.StateReason = &contract.SessionStateReason{
		Code:        "network_unavailable",
		Description: "network unavailable",
	}
	result.SelectedNetworkBinding = &contract.SessionNetworkBinding{
		DisplayName:      "Campus Ethernet",
		InterfaceID:      "if-7",
		LocalIPv4Address: "192.0.2.25",
	}
	result.AuthenticationEstablishedAt = &authenticatedAt
	result.NextRetryAt = &retryAt
	result.LastAuthenticationFailure = &contract.SessionAuthenticationFailure{
		Code:                   "network_timeout",
		Description:            "network operation timed out",
		HandlingRecommendation: "retry_after_standard_delay",
	}

	var output bytes.Buffer
	if err := terminal.WriteSessionResult(&output, result); err != nil {
		t.Fatalf("terminal.WriteSessionResult = %v, want nil", err)
	}
	want := "" +
		"会话：session-1\n" +
		"状态：等待重试（waiting_before_retry）\n" +
		"机构：Example University（profile-1）\n" +
		"协议：protocol-1\n" +
		"账号：account-name\n" +
		"原因：没有可用网络（network_unavailable）\n" +
		"网络：Campus Ethernet — 192.0.2.25\n" +
		"认证时间：2026-07-26T10:11:12.123456789+08:00\n" +
		"下次重试：2026-07-26T10:11:17.123456789+08:00\n" +
		"最近失败：网络操作超时（network_timeout）\n" +
		"处理建议：将自动稍后重试（retry_after_standard_delay）\n" +
		"更新时间：2026-07-26T10:11:12.123456789+08:00\n"
	if output.String() != want {
		t.Errorf("complete output = %q, want %q", output.String(), want)
	}
}

func TestWriteSessionResultMinimalOutput(t *testing.T) {
	var output bytes.Buffer
	if err := terminal.WriteSessionResult(&output, clitest.MinimalSessionResult("authenticated")); err != nil {
		t.Fatalf("terminal.WriteSessionResult = %v, want nil", err)
	}
	want := "" +
		"会话：session-1\n" +
		"状态：已认证（authenticated）\n" +
		"机构：Profile（profile-1）\n" +
		"协议：protocol-1\n" +
		"账号：account-name\n" +
		"更新时间：2026-07-26T10:11:12.123456789+08:00\n"
	if output.String() != want {
		t.Errorf("minimal output = %q, want %q", output.String(), want)
	}
}

func TestSessionResponseFailuresProduceNoSuccessBlock(t *testing.T) {
	tests := []struct {
		name     string
		response contract.Response
	}{
		{
			name:     "malformed JSON",
			response: contract.NewSuccessResponse("1", json.RawMessage("{")),
		},
		{
			name: "daemon error",
			response: contract.NewErrorResponse(
				"1",
				contract.ErrorCodeSessionOperationFailed,
				"session operation failed",
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := &clitest.FakeDaemonClient{
				CallFunc: func(string, json.RawMessage) (contract.Response, error) {
					return test.response, nil
				},
			}
			deps := hotAuthDependencies(t, connection)
			var output bytes.Buffer
			deps.Stdout = &output

			err := runAuthStatus("session-1", deps)
			if err == nil {
				t.Fatal("runAuthStatus = nil, want error")
			}
			if output.Len() != 0 {
				t.Error("failed response produced a success block")
			}
			if connection.CloseCount != 1 {
				t.Errorf("Close count = %d, want 1", connection.CloseCount)
			}
		})
	}
}

func TestMalformedRequiredSessionFieldsProduceNoOutput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*contract.SessionResult)
	}{
		{name: "SessionID", mutate: func(result *contract.SessionResult) { result.AuthenticationSessionID = "" }},
		{name: "state", mutate: func(result *contract.SessionResult) { result.State = "" }},
		{name: "Profile ID", mutate: func(result *contract.SessionResult) { result.InstitutionProfileID = "" }},
		{name: "protocol ID", mutate: func(result *contract.SessionResult) { result.AuthenticationProtocolID = "" }},
		{name: "account label", mutate: func(result *contract.SessionResult) { result.AccountName = "" }},
		{name: "updated timestamp", mutate: func(result *contract.SessionResult) { result.UpdatedAt = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := clitest.MinimalSessionResult("authenticated")
			test.mutate(&result)
			var output bytes.Buffer
			if err := terminal.WriteSessionResult(&output, result); err == nil {
				t.Fatal("terminal.WriteSessionResult = nil, want validation error")
			}
			if output.Len() != 0 {
				t.Error("malformed result produced partial success output")
			}
		})
	}
}

func TestSessionWriterFailureIsReturned(t *testing.T) {
	cause := errors.New("injected writer failure")
	err := terminal.WriteSessionResult(clitest.ZeroWriter{Err: cause}, clitest.MinimalSessionResult("authenticated"))
	if err == nil {
		t.Fatal("terminal.WriteSessionResult = nil, want error")
	}
	if !errors.Is(err, cause) {
		t.Error("terminal.WriteSessionResult did not preserve writer cause")
	}
}

func TestAllPublicSessionStatesAreSuccessfulCommandData(t *testing.T) {
	states := []string{
		"waiting_for_network",
		"authenticating",
		"authenticated",
		"waiting_before_retry",
		"blocked_by_error",
		"stopping",
		"suspended",
	}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			connection := &clitest.FakeDaemonClient{
				CallFunc: func(string, json.RawMessage) (contract.Response, error) {
					return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult(state)), nil
				},
			}
			deps := hotAuthDependencies(t, connection)
			var output bytes.Buffer
			deps.Stdout = &output
			if err := runAuthStatus("session-1", deps); err != nil {
				t.Fatalf("runAuthStatus = %v, want nil for Session state", err)
			}
			if !strings.Contains(output.String(), "（"+state+"）") {
				t.Error("rendered output omitted exact Session state")
			}
		})
	}
}

func TestTransportFailureClosesClientAndPreservesCauseSafely(t *testing.T) {
	cause := errors.New("injected transport failure")
	connection := &clitest.FakeDaemonClient{
		CallFunc: func(string, json.RawMessage) (contract.Response, error) {
			return contract.Response{}, cause
		},
	}
	deps := hotAuthDependencies(t, connection)
	err := runAuthStatus("session-1", deps)
	if err == nil {
		t.Fatal("runAuthStatus = nil, want error")
	}
	if !errors.Is(err, cause) {
		t.Error("runAuthStatus did not preserve transport cause")
	}
	if err.Error() != "调用 Session 操作" {
		t.Errorf("transport error = %q, want static operation label", err)
	}
	if connection.CloseCount != 1 {
		t.Errorf("Close count = %d, want 1", connection.CloseCount)
	}
}

// TestAuthCommandsDoNotCallDaemonStop proves auth commands never invoke the
// daemon.stop lifecycle method, which would stop the daemon out from under an
// active Session.
func TestAuthCommandsDoNotCallDaemonStop(t *testing.T) {
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, _ json.RawMessage) (contract.Response, error) {
		if method == contract.MethodDaemonStop {
			t.Errorf("auth command must not call daemon.stop")
		}
		return clitest.SuccessSessionResponse(t, clitest.MinimalSessionResult("authenticated")), nil
	}}
	deps := hotAuthDependencies(t, connection)
	if err := runAuthStatus("session-1", deps); err != nil {
		t.Fatalf("runAuthStatus: %v", err)
	}
}

func TestReadOnlyConnectionFailureUsesFixedGuidanceAndNoSuccessOutput(t *testing.T) {
	var output bytes.Buffer
	deps := hotAuthDependencies(t, &clitest.FakeDaemonClient{})
	deps.Stdout = &output
	deps.connection.Acquire = func(context.Context) (daemonconn.Client, error) {
		return nil, errors.New("private-runtime-marker")
	}
	err := runAuthStatus("session-1", deps)
	if err == nil || err.Error() != "无法连接 sidraviad（请确认 daemon 已启动；若刚增删过 sidravia.portable 标记，请先停止并重启 daemon）" {
		t.Fatalf("error=%q", clitest.ErrorString(err))
	}
	if output.Len() != 0 || strings.Contains(err.Error(), "private-runtime-marker") {
		t.Fatalf("output=%q error=%q", output.String(), err.Error())
	}
}

func TestBindingFlagsRejectBeforeDaemonOrPassword(t *testing.T) {
	deps := dependencies{connection: daemonconn.Connection{Acquire: func(context.Context) (daemonconn.Client, error) {
		t.Fatal("invalid binding acquired daemon")
		return nil, nil
	}}, IO: terminal.IO{ReadStdinPassword: func(io.Reader) (string, error) { t.Fatal("invalid binding read password"); return "", nil }}}
	for _, binding := range []bindingargs.Flags{{InterfaceSet: true, InterfaceID: "lo"}, {InterfaceSet: true, AddressSet: true, InterfaceID: "", LocalIPv4: "127.0.0.1"}, {InterfaceSet: true, AddressSet: true, InterfaceID: "lo", LocalIPv4: "::1"}} {
		if runAuthStart(StartOptions{Binding: binding, ProfileID: "p", Username: "u", PasswordStdin: true}, deps) == nil {
			t.Fatal("invalid one-shot accepted")
		}
	}
	valid := bindingargs.Flags{InterfaceSet: true, AddressSet: true, InterfaceID: "lo", LocalIPv4: "127.0.0.1"}
	if runAuthStart(StartOptions{Binding: valid, SessionID: "s"}, deps) == nil {
		t.Fatal("bound ensure accepted")
	}
	if policy, err := (bindingargs.Flags{}).Policy(true); err != nil || policy != nil {
		t.Fatal("omitted update did not preserve")
	}
	if policy, err := (bindingargs.Flags{Automatic: true}).Policy(true); err != nil || policy.Mode != "automatically_select_latest_available" {
		t.Fatal("automatic reset failed")
	}
}

func TestSessionCLIDecodersUseExactCompleteContract(t *testing.T) {
	value := clitest.MinimalSessionResult("suspended")
	value.Revision = ^uint64(0)
	raw, err := contract.MarshalSessionResult(value)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSessionResult(raw)
	if err != nil || got.Revision != value.Revision || got.ProtocolSocket.State != "not_observed" {
		t.Fatal("CLI lost full actor facts", err)
	}
	for _, token := range []string{"-0", "0", "1.0", "1e0", "18446744073709551616", "null", `"1"`} {
		if _, err := decodeSessionResult([]byte(strings.Replace(string(raw), `"revision":18446744073709551615`, `"revision":`+token, 1))); err == nil {
			t.Fatal("CLI accepted noncanonical revision", token)
		}
	}
	if _, err := decodeSessionResult([]byte(strings.Replace(string(raw), `"protocolSocket":`, `"privateSocket":`, 1))); err == nil {
		t.Fatal("CLI accepted missing socket")
	}
	start, err := contract.MarshalSessionStartResult(contract.SessionStartResult{Outcome: "resumed", Session: value})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := decodeSessionStartResult(start)
	if err != nil || operation.Session.Revision != value.Revision {
		t.Fatal("CLI operation diverged", err)
	}
}
