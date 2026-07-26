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
	callCount  int
	closeCount int
	call       func(string, json.RawMessage) (contract.Response, error)
	closeErr   error
}

func (client *fakeDaemonClient) Call(
	_ context.Context,
	method string,
	payload json.RawMessage,
) (contract.Response, error) {
	client.callCount++
	return client.call(method, payload)
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
		AccountLabel:             "account-label",
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

func hotAuthDependencies(t *testing.T, connection daemonClient) authDependencies {
	t.Helper()
	return authDependencies{
		discovery: discoveryDependencies{
			runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
			readRuntimeInfo: func(string) (contract.RuntimeInfo, error) {
				return testRuntimeInfo(901), nil
			},
			startDaemon:  func() error { t.Fatal("hot discovery started daemon"); return nil },
			totalWait:    time.Second,
			pollInterval: time.Millisecond,
		},
		connect: func(context.Context, contract.RuntimeInfo) (daemonClient, error) {
			return connection, nil
		},
		callTimeout: time.Second,
		stdin:       strings.NewReader(""),
		stdout:      io.Discard,
		stderr:      io.Discard,
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
	connects := 0
	deps.connect = func(context.Context, contract.RuntimeInfo) (daemonClient, error) {
		connects++
		return connection, nil
	}

	if err := runAuthStatus("session-1", deps); err != nil {
		t.Fatalf("runAuthStatus = %v, want nil", err)
	}
	if connects != 1 {
		t.Errorf("connect calls = %d, want 1", connects)
	}
	if connection.callCount != 1 {
		t.Errorf("Session calls = %d, want 1", connection.callCount)
	}
	if connection.closeCount != 1 {
		t.Errorf("Close calls = %d, want 1", connection.closeCount)
	}
}

func TestAuthDiscoveryStartsOnceAfterStaleClientWithoutCallingSession(t *testing.T) {
	staleInfo := testRuntimeInfo(902)
	freshInfo := testRuntimeInfo(903)
	staleClient := &fakeDaemonClient{
		call: func(string, json.RawMessage) (contract.Response, error) {
			t.Fatal("Session operation ran on failed connection")
			return contract.Response{}, nil
		},
	}
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
			return successSessionResponse(t, minimalSessionResult("suspended")), nil
		},
	}

	reads, connects, starts := 0, 0, 0
	deps := hotAuthDependencies(t, freshClient)
	deps.discovery = discoveryDependencies{
		runtimeInfoPath: func() (string, error) { return "runtime-path", nil },
		readRuntimeInfo: func(string) (contract.RuntimeInfo, error) {
			reads++
			if reads == 1 {
				return staleInfo, nil
			}
			return freshInfo, nil
		},
		startDaemon: func() error {
			starts++
			return nil
		},
		totalWait:    time.Second,
		pollInterval: time.Millisecond,
	}
	deps.connect = func(_ context.Context, info contract.RuntimeInfo) (daemonClient, error) {
		connects++
		if info.PID == staleInfo.PID {
			return staleClient, errors.New("injected stale connection")
		}
		return freshClient, nil
	}

	if err := runAuthStop("session-1", deps); err != nil {
		t.Fatalf("runAuthStop = %v, want nil", err)
	}
	if starts != 1 {
		t.Errorf("startDaemon calls = %d, want 1", starts)
	}
	if connects != 2 {
		t.Errorf("connect calls = %d, want 2", connects)
	}
	if staleClient.callCount != 0 {
		t.Errorf("stale Session calls = %d, want 0", staleClient.callCount)
	}
	if staleClient.closeCount != 1 {
		t.Errorf("stale Close calls = %d, want 1", staleClient.closeCount)
	}
	if freshClient.callCount != 1 {
		t.Errorf("fresh Session calls = %d, want 1", freshClient.callCount)
	}
	if freshClient.closeCount != 1 {
		t.Errorf("fresh Close calls = %d, want 1", freshClient.closeCount)
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
			return successSessionResponse(t, minimalSessionResult("authenticating")), nil
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
			return successSessionResponse(t, minimalSessionResult("waiting_for_network")), nil
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
			resultState: "suspended",
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
		Code:        "network_changed",
		Description: "network selection changed",
	}
	result.SelectedNetworkBinding = &contract.SessionNetworkBinding{
		DisplayName:      "Campus Ethernet",
		InterfaceID:      "if-7",
		LocalIPv4Address: "192.0.2.25",
	}
	result.AuthenticationEstablishedAt = &authenticatedAt
	result.NextRetryAt = &retryAt
	result.LastAuthenticationFailure = &contract.SessionAuthenticationFailure{
		Code:                   "temporary_network_failure",
		Description:            "authentication transport failed",
		HandlingRecommendation: "retry_after_standard_delay",
	}

	var output bytes.Buffer
	if err := writeSessionResult(&output, result); err != nil {
		t.Fatalf("writeSessionResult = %v, want nil", err)
	}
	want := "" +
		"Session: session-1\n" +
		"State: waiting_before_retry\n" +
		"Profile: Example University (profile-1)\n" +
		"Protocol: protocol-1\n" +
		"Account: account-label\n" +
		"Reason: network_changed — network selection changed\n" +
		"Network: Campus Ethernet [if-7] — 192.0.2.25\n" +
		"Authenticated: 2026-07-26T10:11:12.123456789+08:00\n" +
		"Retry: 2026-07-26T10:11:17.123456789+08:00\n" +
		"Failure: temporary_network_failure — authentication transport failed (retry_after_standard_delay)\n" +
		"Updated: 2026-07-26T10:11:12.123456789+08:00\n"
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
		"Session: session-1\n" +
		"State: authenticated\n" +
		"Profile: profile-1\n" +
		"Protocol: protocol-1\n" +
		"Account: account-label\n" +
		"Updated: 2026-07-26T10:11:12.123456789+08:00\n"
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
		{name: "account label", mutate: func(result *contract.SessionResult) { result.AccountLabel = "" }},
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
			if !strings.Contains(output.String(), "State: "+state+"\n") {
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
	if err.Error() != "call Session operation" {
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
