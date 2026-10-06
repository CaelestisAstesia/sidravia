package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

type fakeDaemonClient struct {
	callCount   int
	closeCount  int
	call        func(string, json.RawMessage) (contract.Response, error)
	callContext func(context.Context)
	closeErr    error
}

func TestSessionStartClearsOwnedWireBytesOnEveryExit(t *testing.T) {
	cause := errors.New("fictional transport failure")
	for _, outcome := range []string{"success", "transport", "typed error", "decode error"} {
		t.Run(outcome, func(t *testing.T) {
			var retained json.RawMessage
			connection := &fakeDaemonClient{call: func(method string, buffer json.RawMessage) (contract.Response, error) {
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
					return successSessionStartResponse(t, "created", minimalSessionResult("authenticated")), nil
				}
			}}
			deps := hotAuthDependencies(t, connection)
			_, err := callSessionStart(deps, connection, contract.MethodSessionStartOneShot, contract.SessionStartOneShotPayload{
				InstitutionProfileID: "jlu", Username: "user", Password: "fictional",
				NetworkBindingPolicyMode: "automatic", ProtocolContextOverride: json.RawMessage(`{}`),
			})
			if (err == nil) != (outcome == "success") || (outcome == "transport" && !errors.Is(err, cause)) {
				t.Fatal("operation error behavior changed")
			}
			assertClearedBytes(t, retained)
		})
	}
}

func TestRunAuthStartRetainedDoesNotReadPassword(t *testing.T) {
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionEnsureRunning || string(payload) != `{"sessionId":"session-1"}` {
			t.Fatalf("call = %s %s", method, payload)
		}
		return successSessionStartResponse(t, "already_running", minimalSessionResult("authenticated")), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.readStdinPassword = func(io.Reader) (string, error) { t.Fatal("stdin password read"); return "", nil }
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) { t.Fatal("interactive password read"); return "", nil }
	if err := runAuthStart(authStartOptions{sessionID: "session-1"}, deps); err != nil {
		t.Fatal(err)
	}
}

func TestRunAuthStartRetainedRejectsRetiredBareSessionResult(t *testing.T) {
	var output bytes.Buffer
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionEnsureRunning {
			t.Fatalf("method = %q", method)
		}
		return successSessionResponse(t, minimalSessionResult("authenticated")), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.stdout = &output
	if err := runAuthStart(authStartOptions{sessionID: "session-1"}, deps); err == nil {
		t.Fatal("retired bare result succeeded")
	}
	if output.Len() != 0 {
		t.Fatalf("retired bare result produced success output: %q", output.String())
	}
}

func TestRunAuthStartConfigurationTypedRequestDeadlinePresentationAndNoPasswordRead(t *testing.T) {
	var output bytes.Buffer
	connection := &fakeDaemonClient{
		callContext: func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("configuration start has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining < 29*time.Second || remaining > 31*time.Second {
				t.Fatalf("configuration start deadline = %v", remaining)
			}
		},
		call: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionStartConfiguration ||
				string(payload) != `{"configurationId":"campus"}` {
				t.Fatalf("call = %s %s", method, payload)
			}
			result := minimalSessionResult("authenticated")
			result.AuthenticationSessionID = "configuration-session"
			result.InstitutionDisplayName = "Campus"
			return successSessionStartResponse(t, "created", result), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	deps.stdout = &output
	deps.readStdinPassword = func(io.Reader) (string, error) {
		t.Fatal("config start read stdin password")
		return "", nil
	}
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) {
		t.Fatal("config start read interactive password")
		return "", nil
	}
	if err := runAuthStart(authStartOptions{configurationID: "campus"}, deps); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"会话：configuration-session", "状态：已认证", "机构：Campus"} {
		if !strings.Contains(output.String(), text) {
			t.Fatalf("presentation %q missing %q", output.String(), text)
		}
	}
}

func (client *fakeDaemonClient) Call(
	ctx context.Context,
	method string,
	payload json.RawMessage,
) (contract.Response, error) {
	client.callCount++
	if client.callContext != nil {
		client.callContext(ctx)
	}
	return client.call(method, payload)
}

func TestRetainedLifecycleDispatchPayloadAndDeadline(t *testing.T) {
	tests := []struct {
		name   string
		method string
		run    func(string, authDependencies) error
		result func(t *testing.T) contract.Response
	}{
		{"ensure", contract.MethodSessionEnsureRunning, func(id string, deps authDependencies) error {
			return runAuthStart(authStartOptions{sessionID: id}, deps)
		}, func(t *testing.T) contract.Response {
			return successSessionStartResponse(t, "already_running", minimalSessionResult("authenticated"))
		}},
		{"restart", contract.MethodSessionRestart, runAuthRestart, func(t *testing.T) contract.Response {
			return successSessionResponse(t, minimalSessionResult("authenticating"))
		}},
		{"remove", contract.MethodSessionRemove, runAuthRemove, func(t *testing.T) contract.Response {
			return contract.NewSuccessResponse("1", json.RawMessage(`{"sessionId":"session-1","status":"removed"}`))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
				if method != test.method || string(payload) != `{"sessionId":"session-1"}` {
					t.Fatalf("call=%s %s", method, payload)
				}
				return test.result(t), nil
			}}
			connection.callContext = func(ctx context.Context) {
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

func TestDecodeSessionRemoveResultStrict(t *testing.T) {
	valid, err := decodeSessionRemoveResult([]byte(`{"sessionId":"session-1","status":"removed"}`))
	if err != nil || valid.SessionID != "session-1" {
		t.Fatalf("valid remove result=%#v err=%v", valid, err)
	}
	for _, data := range []string{
		`{"sessionId":"","status":"removed"}`,
		`{"sessionId":"session-1","status":"wrong"}`,
		`{"sessionId":"session-1","status":"removed","extra":true}`,
		`{"sessionId":"session-1","status":"removed"}{}`,
	} {
		if _, err := decodeSessionRemoveResult([]byte(data)); err == nil {
			t.Fatalf("accepted invalid remove result %s", data)
		}
	}
}

func TestRunAuthRemovePreservesTransportAndWriteCauses(t *testing.T) {
	transportCause := errors.New("transport-cause")
	connection := &fakeDaemonClient{call: func(string, json.RawMessage) (contract.Response, error) {
		return contract.Response{}, transportCause
	}}
	if err := runAuthRemove("session-1", hotAuthDependencies(t, connection)); !errors.Is(err, transportCause) {
		t.Fatalf("transport cause not preserved: %v", err)
	}

	writeCause := errors.New("write-cause")
	connection = &fakeDaemonClient{call: func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewSuccessResponse("1", json.RawMessage(`{"sessionId":"session-1","status":"removed"}`)), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.stdout = zeroWriter{err: writeCause}
	if err := runAuthRemove("session-1", deps); !errors.Is(err, writeCause) {
		t.Fatalf("write cause not preserved: %v", err)
	}

	const publicMarker = "daemon-public-marker"
	const wrappedMarker = "daemon-wrapped-marker"
	connection = &fakeDaemonClient{call: func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewErrorResponse("1", contract.ErrorCodeSessionOperationFailed, publicMarker), errors.New(wrappedMarker)
	}}
	var output bytes.Buffer
	deps = hotAuthDependencies(t, connection)
	deps.stdout = &output
	err := runAuthRemove("session-1", deps)
	if err == nil {
		t.Fatal("daemon error returned nil")
	}
	if strings.Contains(err.Error(), publicMarker) || strings.Contains(err.Error(), wrappedMarker) ||
		strings.Contains(output.String(), publicMarker) || strings.Contains(output.String(), wrappedMarker) {
		t.Fatalf("daemon failure leaked markers: err=%q output=%q", err, output.String())
	}
}

func (client *fakeDaemonClient) Close() error {
	client.closeCount++
	return client.closeErr
}

func minimalSessionResult(state string) contract.SessionResult {
	return contract.SessionResult{
		AuthenticationSessionID:  "session-1",
		InstitutionProfileID:     "profile-1",
		AuthenticationProtocolID: "protocol-1",
		AccountName:              "account-name",
		State:                    state,
		UpdatedAt:                "2026-07-26T10:11:12.123456789+08:00",
	}
}

func successSessionResponse(t *testing.T, result contract.SessionResult) contract.Response {
	t.Helper()
	payload, err := contract.MarshalSessionResult(result)
	if err != nil {
		t.Fatalf("MarshalSessionResult = %v", err)
	}
	return contract.NewSuccessResponse("1", payload)
}

func successSessionStartResponse(t *testing.T, outcome string, result contract.SessionResult) contract.Response {
	t.Helper()
	payload, err := contract.MarshalSessionStartResult(contract.SessionStartResult{Outcome: outcome, Session: result})
	if err != nil {
		t.Fatalf("MarshalSessionStartResult = %v", err)
	}
	return contract.NewSuccessResponse("1", payload)
}

func hotAuthDependencies(t *testing.T, connection daemonClient) authDependencies {
	t.Helper()
	return authDependencies{
		connection: daemonConnectionDependencies{
			acquire: func(context.Context) (daemonClient, error) {
				return connection, nil
			},
			callTimeout: time.Second,
		},
		stdin:  strings.NewReader(""),
		stdout: io.Discard,
		stderr: io.Discard,
		readStdinPassword: func(io.Reader) (string, error) {
			t.Fatal("unexpected stdin password read")
			return "", nil
		},
		readInteractivePassword: func(io.Reader, io.Writer) (string, error) {
			t.Fatal("unexpected interactive password read")
			return "", nil
		},
	}
}

func TestAuthDiscoveryReturnsHotClientAndClosesIt(t *testing.T) {
	connection := &fakeDaemonClient{
		call: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionGet {
				t.Errorf("method = %q, want session.get", method)
			}
			return successSessionResponse(t, minimalSessionResult("authenticated")), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	acquires := 0
	deps.connection.acquire = func(context.Context) (daemonClient, error) {
		acquires++
		return connection, nil
	}

	if err := runAuthStatus("session-1", deps); err != nil {
		t.Fatalf("runAuthStatus = %v, want nil", err)
	}
	if acquires != 1 {
		t.Errorf("acquire calls = %d, want 1", acquires)
	}
	if connection.callCount != 1 {
		t.Errorf("Session calls = %d, want 1", connection.callCount)
	}
	if connection.closeCount != 1 {
		t.Errorf("Close calls = %d, want 1", connection.closeCount)
	}
}

func TestAuthBootstrapFailureDoesNotDispatchSession(t *testing.T) {
	freshClient := &fakeDaemonClient{
		call: func(method string, payload json.RawMessage) (contract.Response, error) {
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
			return successSessionResponse(t, minimalSessionResult("stopping")), nil
		},
	}

	deps := hotAuthDependencies(t, freshClient)
	deferred := true
	deps.connection.acquire = func(context.Context) (daemonClient, error) {
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
	if freshClient.callCount != 0 {
		t.Fatalf("session call count=%d", freshClient.callCount)
	}
}

func TestAuthStartInteractiveTypedRequestAndSecrecy(t *testing.T) {
	username := "username-secret-marker"
	password := "password-secret-marker"
	var method string
	var rawPayload json.RawMessage
	connection := &fakeDaemonClient{
		call: func(gotMethod string, gotPayload json.RawMessage) (contract.Response, error) {
			method = gotMethod
			rawPayload = append(json.RawMessage(nil), gotPayload...)
			return successSessionStartResponse(t, "created", minimalSessionResult("authenticating")), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	var stdout, stderr bytes.Buffer
	deps.stdout = &stdout
	deps.stderr = &stderr
	interactiveReads, stdinReads := 0, 0
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) {
		interactiveReads++
		return password, nil
	}
	deps.readStdinPassword = func(io.Reader) (string, error) {
		stdinReads++
		return "", nil
	}

	err := runAuthStart(authStartOptions{
		profileID: "profile-1",
		username:  username,
	}, deps)
	if err != nil {
		t.Fatalf("runAuthStart = %v, want nil", err)
	}
	if method != contract.MethodSessionStartOneShot {
		t.Errorf("method = %q, want session.startOneShot", method)
	}
	if connection.callCount != 1 {
		t.Errorf("Call count = %d, want 1", connection.callCount)
	}
	if connection.closeCount != 1 {
		t.Errorf("Close count = %d, want 1", connection.closeCount)
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
		decoded.NetworkBindingPolicyMode != automaticNetworkBindingPolicy ||
		string(decoded.ProtocolContextOverride) != "{}" {
		t.Error("start payload non-secret fields differ from accepted values")
	}
	if decoded.Username != username || decoded.Password != password {
		t.Error("start payload credential fields differ from accepted input")
	}
	for _, visible := range []string{stdout.String(), stderr.String(), errorString(err)} {
		if strings.Contains(visible, username) || strings.Contains(visible, password) {
			t.Error("interactive start exposed a credential marker")
		}
	}
}

func TestAuthStartStdinTypedRequestAndSecrecy(t *testing.T) {
	username := "stdin-username-marker"
	password := "stdin-password-marker"
	var rawPayload json.RawMessage
	connection := &fakeDaemonClient{
		call: func(method string, payload json.RawMessage) (contract.Response, error) {
			if method != contract.MethodSessionStartOneShot {
				t.Errorf("method = %q, want session.startOneShot", method)
			}
			rawPayload = append(json.RawMessage(nil), payload...)
			return successSessionStartResponse(t, "created", minimalSessionResult("waiting_for_network")), nil
		},
	}
	deps := hotAuthDependencies(t, connection)
	var stdout, stderr bytes.Buffer
	deps.stdout = &stdout
	deps.stderr = &stderr
	stdinReads, interactiveReads := 0, 0
	deps.readStdinPassword = func(io.Reader) (string, error) {
		stdinReads++
		return password, nil
	}
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) {
		interactiveReads++
		return "", nil
	}

	err := runAuthStart(authStartOptions{
		profileID:     "profile-1",
		username:      username,
		passwordStdin: true,
	}, deps)
	if err != nil {
		t.Fatalf("runAuthStart = %v, want nil", err)
	}
	if connection.callCount != 1 {
		t.Errorf("Call count = %d, want 1", connection.callCount)
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
		decoded.NetworkBindingPolicyMode != automaticNetworkBindingPolicy ||
		string(decoded.ProtocolContextOverride) != "{}" {
		t.Error("stdin start payload differs from accepted input")
	}
	for _, visible := range []string{stdout.String(), stderr.String(), errorString(err)} {
		if strings.Contains(visible, username) || strings.Contains(visible, password) {
			t.Error("stdin start exposed a credential marker")
		}
	}
}

func TestAuthStatusAndStopTypedRequestsWithoutPasswordRead(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		run         func(string, authDependencies) error
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
			connection := &fakeDaemonClient{
				call: func(method string, payload json.RawMessage) (contract.Response, error) {
					if method != test.method {
						t.Errorf("method = %q, want %q", method, test.method)
					}
					rawPayload = append(json.RawMessage(nil), payload...)
					return successSessionResponse(t, minimalSessionResult(test.resultState)), nil
				},
			}
			deps := hotAuthDependencies(t, connection)
			passwordReads := 0
			deps.readStdinPassword = func(io.Reader) (string, error) {
				passwordReads++
				return "", nil
			}
			deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) {
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
			if connection.callCount != 1 {
				t.Errorf("Call count = %d, want 1", connection.callCount)
			}
			if connection.closeCount != 1 {
				t.Errorf("Close count = %d, want 1", connection.closeCount)
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
	result := minimalSessionResult("waiting_before_retry")
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
	if err := writeSessionResult(&output, result); err != nil {
		t.Fatalf("writeSessionResult = %v, want nil", err)
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
	if err := writeSessionResult(&output, minimalSessionResult("authenticated")); err != nil {
		t.Fatalf("writeSessionResult = %v, want nil", err)
	}
	want := "" +
		"会话：session-1\n" +
		"状态：已认证（authenticated）\n" +
		"机构：profile-1\n" +
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
			connection := &fakeDaemonClient{
				call: func(string, json.RawMessage) (contract.Response, error) {
					return test.response, nil
				},
			}
			deps := hotAuthDependencies(t, connection)
			var output bytes.Buffer
			deps.stdout = &output

			err := runAuthStatus("session-1", deps)
			if err == nil {
				t.Fatal("runAuthStatus = nil, want error")
			}
			if output.Len() != 0 {
				t.Error("failed response produced a success block")
			}
			if connection.closeCount != 1 {
				t.Errorf("Close count = %d, want 1", connection.closeCount)
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
			result := minimalSessionResult("authenticated")
			test.mutate(&result)
			var output bytes.Buffer
			if err := writeSessionResult(&output, result); err == nil {
				t.Fatal("writeSessionResult = nil, want validation error")
			}
			if output.Len() != 0 {
				t.Error("malformed result produced partial success output")
			}
		})
	}
}

func TestSessionWriterFailureIsReturned(t *testing.T) {
	cause := errors.New("injected writer failure")
	err := writeSessionResult(zeroWriter{err: cause}, minimalSessionResult("authenticated"))
	if err == nil {
		t.Fatal("writeSessionResult = nil, want error")
	}
	if !errors.Is(err, cause) {
		t.Error("writeSessionResult did not preserve writer cause")
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
			connection := &fakeDaemonClient{
				call: func(string, json.RawMessage) (contract.Response, error) {
					return successSessionResponse(t, minimalSessionResult(state)), nil
				},
			}
			deps := hotAuthDependencies(t, connection)
			var output bytes.Buffer
			deps.stdout = &output
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
	connection := &fakeDaemonClient{
		call: func(string, json.RawMessage) (contract.Response, error) {
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
	if connection.closeCount != 1 {
		t.Errorf("Close count = %d, want 1", connection.closeCount)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type zeroWriter struct {
	err error
}

func (writer zeroWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

// TestAuthCommandsDoNotCallDaemonStop proves auth commands never invoke the
// daemon.stop lifecycle method, which would stop the daemon out from under an
// active Session.
func TestAuthCommandsDoNotCallDaemonStop(t *testing.T) {
	connection := &fakeDaemonClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
		if method == contract.MethodDaemonStop {
			t.Errorf("auth command must not call daemon.stop")
		}
		return successSessionResponse(t, minimalSessionResult("authenticated")), nil
	}}
	deps := hotAuthDependencies(t, connection)
	if err := runAuthStatus("session-1", deps); err != nil {
		t.Fatalf("runAuthStatus: %v", err)
	}
}

func TestReadOnlyConnectionFailureUsesFixedGuidanceAndNoSuccessOutput(t *testing.T) {
	var output bytes.Buffer
	deps := hotAuthDependencies(t, &fakeDaemonClient{})
	deps.stdout = &output
	deps.connection.acquire = func(context.Context) (daemonClient, error) {
		return nil, errors.New("private-runtime-marker")
	}
	err := runAuthStatus("session-1", deps)
	if err == nil || err.Error() != "无法连接 sidraviad（请确认 daemon 已启动；若刚增删过 sidravia.portable 标记，请先停止并重启 daemon）" {
		t.Fatalf("error=%q", errorString(err))
	}
	if output.Len() != 0 || strings.Contains(err.Error(), "private-runtime-marker") {
		t.Fatalf("output=%q error=%q", output.String(), err.Error())
	}
}
