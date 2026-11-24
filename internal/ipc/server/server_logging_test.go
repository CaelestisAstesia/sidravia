package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

func TestNormalizeRetainedSessionMethods(t *testing.T) {
	for _, method := range []string{contract.MethodSessionEnsureRunning, contract.MethodSessionRestart, contract.MethodSessionRemove} {
		if got := normalizeMethod(method); got != method {
			t.Fatalf("normalizeMethod(%q) = %q", method, got)
		}
	}
}

func TestNormalizeConfigurationMethodsAndErrors(t *testing.T) {
	for _, method := range []string{
		contract.MethodConfigurationList,
		contract.MethodConfigurationGet,
		contract.MethodConfigurationCreate,
		contract.MethodConfigurationUpdate,
		contract.MethodConfigurationSetPassword,
		contract.MethodConfigurationRemove,
		contract.MethodSessionStartConfiguration,
	} {
		if got := normalizeMethod(method); got != method {
			t.Fatalf("normalizeMethod(%q) = %q", method, got)
		}
	}
	for _, code := range []string{
		contract.ErrorCodeConfigurationNotFound,
		contract.ErrorCodeConfigurationConflict,
		contract.ErrorCodeConfigurationOperationFailed,
		contract.ErrorCodeConfigurationSessionInvalidationFailed,
		contract.ErrorCodeInsecureStorageConfirmationRequired,
		contract.ErrorCodeConfigurationAutoLoginConflict,
		contract.ErrorCodeSessionNotFound,
		contract.ErrorCodeSessionActiveConflict,
		contract.ErrorCodeSessionStateConflict,
	} {
		if got := normalizeErrorCode(code); got != code {
			t.Fatalf("normalizeErrorCode(%q) = %q", code, got)
		}
	}
	if normalizeMethod("configuration.private-marker") != methodUnknown ||
		normalizeErrorCode("private-error-marker") != errorCodeInternal {
		t.Fatal("unallowlisted peer strings were not normalized")
	}
}

func TestConfigurationRequestsLogOnlyAllowlistedMethodAndCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		code   string
	}{
		{"list operation failure", contract.MethodConfigurationList, contract.ErrorCodeConfigurationOperationFailed},
		{"get not found", contract.MethodConfigurationGet, contract.ErrorCodeConfigurationNotFound},
		{"create conflict", contract.MethodConfigurationCreate, contract.ErrorCodeConfigurationConflict},
		{"create insecure storage confirmation", contract.MethodConfigurationCreate, contract.ErrorCodeInsecureStorageConfirmationRequired},
		{"update auto login conflict", contract.MethodConfigurationUpdate, contract.ErrorCodeConfigurationAutoLoginConflict},
		{"update operation failure", contract.MethodConfigurationUpdate, contract.ErrorCodeConfigurationOperationFailed},
		{"set password insecure storage confirmation", contract.MethodConfigurationSetPassword, contract.ErrorCodeInsecureStorageConfirmationRequired},
		{"remove not found", contract.MethodConfigurationRemove, contract.ErrorCodeConfigurationNotFound},
		{"start configuration session operation failure", contract.MethodSessionStartConfiguration, contract.ErrorCodeSessionOperationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf safeBuffer
			const (
				requestIDMarker = "private-request-id-marker"
				configIDMarker  = "private-configuration-id-marker"
				usernameMarker  = "private-username-marker"
				passwordMarker  = "private-password-marker"
				payloadMarker   = "private-payload-marker"
				causeMarker     = "private-wrapped-cause-marker"
			)
			handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
				return nil, &contract.Error{Code: tc.code, Message: "failed: " + causeMarker}
			}
			_, wsURL := newTestServer(t, handler, &buf)
			conn := dial(t, wsURL)
			defer conn.Close(websocket.StatusNormalClosure, "")
			writeRequest(t, conn, requestIDMarker, tc.method, json.RawMessage(
				`{"configurationId":"`+configIDMarker+`","username":"`+usernameMarker+
					`","password":"`+passwordMarker+`","marker":"`+payloadMarker+`"}`,
			))
			response := readResponse(t, conn)
			if response.OK || response.Error == nil || response.Error.Code != tc.code {
				t.Fatalf("response = %#v", response)
			}
			waitForLogEvent(t, &buf, "method="+tc.method)
			waitForLogEvent(t, &buf, "error_code="+tc.code)
			output := buf.String()
			for _, marker := range []string{requestIDMarker, configIDMarker, usernameMarker, passwordMarker, payloadMarker, causeMarker, testToken} {
				if strings.Contains(output, marker) {
					t.Fatalf("log leaked %q: %s", marker, output)
				}
			}
		})
	}
}

func TestRetainedSessionMethodsLogOnlyStableMethod(t *testing.T) {
	var buf safeBuffer
	const idMarker = "request-secret-marker"
	const payloadMarker = "payload-secret-marker"
	const causeMarker = "handler-cause-marker"
	const bearerMarker = testToken
	handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		return nil, &contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "operation failed: " + causeMarker}
	}
	_, wsURL := newTestServer(t, handler, &buf)
	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")
	for _, method := range []string{contract.MethodSessionEnsureRunning, contract.MethodSessionRestart, contract.MethodSessionRemove} {
		writeRequest(t, conn, idMarker, method, json.RawMessage(`{"sessionId":"`+payloadMarker+`"}`))
		if response := readResponse(t, conn); response.OK || response.Error == nil ||
			response.Error.Code != contract.ErrorCodeSessionOperationFailed {
			t.Fatalf("%s response=%#v", method, response)
		}
		waitForLogEvent(t, &buf, "method="+method)
		waitForLogEvent(t, &buf, "code="+string(contract.ErrorCodeSessionOperationFailed))
	}
	output := buf.String()
	for _, marker := range []string{idMarker, payloadMarker, causeMarker, bearerMarker} {
		if strings.Contains(output, marker) {
			t.Fatalf("log leaked %q: %s", marker, output)
		}
	}
}

func TestIPCRequestCompletedOnSuccess(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDaemonStatus {
			return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported"}
		}
		return json.RawMessage(`{"productVersion":"1.0.0","buildId":"b","pid":1,"status":"running"}`), nil
	}
	_, wsURL := newTestServer(t, handler, &buf)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	resp := readResponse(t, conn)
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}

	waitForLogEvent(t, &buf, "event=ipc_request_completed")
	output := buf.String()
	if strings.Count(output, "event=ipc_connection_opened") != 1 {
		t.Fatalf("expected one ipc_connection_opened, got:\n%s", output)
	}
	if strings.Count(output, "event=ipc_request_completed") != 1 {
		t.Fatalf("expected one ipc_request_completed, got:\n%s", output)
	}
	if !strings.Contains(output, "method=daemon.status") {
		t.Fatalf("expected method=daemon.status, got:\n%s", output)
	}
	if strings.Contains(output, "event=ipc_request_rejected") {
		t.Fatalf("success must not log ipc_request_rejected, got:\n%s", output)
	}
}

// TestIPCRequestCompletedDroppedAtInfoLevel proves successful IPC request
// completion and connection-opened events are Debug records: they appear at
// Debug level but are dropped at Info level, while Warn records still appear.
func TestIPCRequestCompletedDroppedAtInfoLevel(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDaemonStatus {
			return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported"}
		}
		return json.RawMessage(`{"productVersion":"1.0.0","buildId":"b","pid":1,"status":"running"}`), nil
	}
	_, wsURL := newTestServerWithLevel(t, handler, &buf, slog.LevelInfo)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Successful request: its Debug events must be dropped at Info level.
	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	readResponse(t, conn)

	// Malformed request: its Warn event appears at Info level, proving the log
	// stream is active and giving a stable synchronization point.
	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"kind":"bad","id":"2"}`)); err != nil {
		t.Fatalf("write malformed request: %v", err)
	}
	readResponse(t, conn)

	waitForLogEvent(t, &buf, "event=ipc_request_rejected")
	output := buf.String()
	if strings.Contains(output, "event=ipc_request_completed") {
		t.Fatalf("Info level must drop Debug ipc_request_completed, got:\n%s", output)
	}
	if strings.Contains(output, "event=ipc_connection_opened") {
		t.Fatalf("Info level must drop Debug ipc_connection_opened, got:\n%s", output)
	}
}

func TestIPCRequestRejectedOnMalformed(t *testing.T) {
	var buf safeBuffer
	handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		t.Fatal("handler must not be called for malformed request")
		return nil, nil
	}
	_, wsURL := newTestServer(t, handler, &buf)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"kind":"bad","id":"1"}`)); err != nil {
		t.Fatalf("write malformed request: %v", err)
	}
	resp := readResponse(t, conn)
	if resp.OK {
		t.Fatal("expected error response for malformed request")
	}
	if resp.Error.Code != contract.ErrorCodeMalformed {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, contract.ErrorCodeMalformed)
	}

	waitForLogEvent(t, &buf, "event=ipc_request_rejected")
	output := buf.String()
	if strings.Count(output, "event=ipc_request_rejected") != 1 {
		t.Fatalf("expected one ipc_request_rejected, got:\n%s", output)
	}
	if !strings.Contains(output, "method=unknown") {
		t.Fatalf("expected method=unknown for malformed request, got:\n%s", output)
	}
	if !strings.Contains(output, "error_code=malformed_request") {
		t.Fatalf("expected error_code=malformed_request, got:\n%s", output)
	}
}

func TestIPCRequestRejectedOnHandlerError(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		return nil, &contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "session op failed"}
	}
	_, wsURL := newTestServer(t, handler, &buf)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	writeRequest(t, conn, "1", contract.MethodSessionGet, json.RawMessage(`{"sessionId":"s1"}`))
	resp := readResponse(t, conn)
	if resp.OK {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != contract.ErrorCodeSessionOperationFailed {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, contract.ErrorCodeSessionOperationFailed)
	}

	waitForLogEvent(t, &buf, "event=ipc_request_rejected")
	output := buf.String()
	if strings.Count(output, "event=ipc_request_rejected") != 1 {
		t.Fatalf("expected one ipc_request_rejected, got:\n%s", output)
	}
	if !strings.Contains(output, "method=session.get") {
		t.Fatalf("expected method=session.get, got:\n%s", output)
	}
	if !strings.Contains(output, "error_code=session_operation_failed") {
		t.Fatalf("expected error_code=session_operation_failed, got:\n%s", output)
	}
}

func TestIPCResponseFailedOnEncode(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		return json.RawMessage(`{"injected-encode-marker-5E"`), nil
	}
	_, wsURL := newTestServer(t, handler, &buf)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	response := readResponse(t, conn)
	if response.ID != "1" || response.OK || response.Error == nil || response.Error.Code != contract.ErrorCodeInternalError || response.Error.Message != "internal error" {
		t.Fatalf("fallback response = %#v", response)
	}

	waitForLogEvent(t, &buf, "event=ipc_response_failed")
	output := buf.String()
	if strings.Count(output, "event=ipc_response_failed") != 1 {
		t.Fatalf("expected one ipc_response_failed, got:\n%s", output)
	}
	if !strings.Contains(output, "stage=encode") {
		t.Fatalf("expected stage=encode, got:\n%s", output)
	}
	if strings.Contains(output, "event=ipc_request_completed") {
		t.Fatalf("encode failure must not log ipc_request_completed, got:\n%s", output)
	}
	if strings.Contains(output, "injected-encode-marker-5E") {
		t.Fatalf("encode error leaked into log, got:\n%s", output)
	}
}

func TestIPCResponseFailedOnWrite(t *testing.T) {
	var buf safeBuffer
	started := make(chan struct{})
	proceed := make(chan struct{})
	handler := func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		close(started)
		<-proceed
		return json.RawMessage(`{}`), nil
	}
	srv, err := NewServer(testToken, testBuild, handler, newTestLogger(&buf), nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	h, accepted := serveTrackedTestHTTP(t, srv)
	wsURL := testWebSocketURL(h.URL)

	conn := dial(t, wsURL)
	t.Cleanup(func() { _ = conn.CloseNow() })
	serverConn := <-accepted
	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	waitForChannel(t, started, "handler start")
	if err := serverConn.Close(); err != nil {
		t.Fatalf("close accepted server connection: %v", err)
	}
	close(proceed)

	waitForLogEvent(t, &buf, "event=ipc_response_failed")
	output := buf.String()
	if strings.Count(output, "event=ipc_response_failed") != 1 {
		t.Fatalf("expected one ipc_response_failed, got:\n%s", output)
	}
	if !strings.Contains(output, "stage=write") {
		t.Fatalf("expected stage=write, got:\n%s", output)
	}
	if strings.Contains(output, "event=ipc_request_completed") {
		t.Fatalf("write failure must not log ipc_request_completed, got:\n%s", output)
	}
}

func TestIPCUpgradeFailed(t *testing.T) {
	var buf safeBuffer
	handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		t.Fatal("handler must not be called on upgrade failure")
		return nil, nil
	}
	_, httpServer := serveTestRPC(t, handler, newTestLogger(&buf), nil)

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/ipc", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Sidravia-Build-ID", testBuild)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	resp.Body.Close()

	waitForLogEvent(t, &buf, "event=ipc_upgrade_failed")
	output := buf.String()
	if strings.Count(output, "event=ipc_upgrade_failed") != 1 {
		t.Fatalf("expected one ipc_upgrade_failed, got:\n%s", output)
	}
	if strings.Contains(output, "event=ipc_connection_opened") {
		t.Fatalf("upgrade failure must not log ipc_connection_opened, got:\n%s", output)
	}
}

func TestMaliciousStringsNeverAppearInLogs(t *testing.T) {
	var buf safeBuffer
	const (
		methodMarker  = "evil.method.MARKER-1"
		codeMarker    = "evil.code.MARKER-2"
		msgMarker     = "evil.message.MARKER-3"
		idMarker      = "secret-request-id.MARKER-4"
		payloadMarker = "payload-secret.MARKER-5"
	)

	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method == methodMarker {
			return nil, &contract.Error{Code: codeMarker, Message: msgMarker}
		}
		return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported"}
	}
	_, wsURL := newTestServer(t, handler, &buf)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	writeRequest(t, conn, idMarker, methodMarker, json.RawMessage(`{"marker":"`+payloadMarker+`"}`))
	readResponse(t, conn)

	waitForLogEvent(t, &buf, "event=ipc_request_rejected")
	output := buf.String()
	for _, marker := range []string{methodMarker, codeMarker, msgMarker, idMarker, payloadMarker, testToken} {
		if strings.Contains(output, marker) {
			t.Fatalf("marker %q leaked into log, got:\n%s", marker, output)
		}
	}
	if !strings.Contains(output, "method=unknown") {
		t.Fatalf("expected method=unknown for malicious method, got:\n%s", output)
	}
	if !strings.Contains(output, "error_code=internal_error") {
		t.Fatalf("expected error_code=internal_error for malicious code, got:\n%s", output)
	}
}

func TestNormalizeOnlyExactNetworkQuery(t *testing.T) {
	if normalizeMethod(contract.MethodNetworkInterfaces) != contract.MethodNetworkInterfaces {
		t.Fatal("network query missing from log allowlist")
	}
	for _, method := range []string{"network.interfaces.private", "network.private-interface-id", "NETWORK.INTERFACES"} {
		if normalizeMethod(method) != methodUnknown {
			t.Fatal("untrusted network method logged")
		}
	}
}

func TestRemovedNetworkDiagnosisMethodIsUnknownWithoutLoggingSelector(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != "network.diagnose" {
			t.Error("diagnosis dispatch changed")
		}
		return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unknown method"}
	}
	_, url := newTestServer(t, handler, &buf)
	conn := dial(t, url)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "diagnostic-private-id", "network.diagnose", json.RawMessage(`{"configurationId":"private-selector"}`))
	response := readResponse(t, conn)
	if response.OK || response.ID != "diagnostic-private-id" || response.Error.Code != contract.ErrorCodeUnknownMethod {
		t.Fatal("removed method was accepted")
	}
	waitForLogEvent(t, &buf, "method="+methodUnknown)
	if strings.Contains(buf.String(), "private-selector") || strings.Contains(buf.String(), "diagnostic-private-id") {
		t.Fatal("diagnosis selector leaked into log")
	}
}

func TestDiagnosticExportMethodAllowlistAndPrivatePayloadLogging(t *testing.T) {
	if normalizeMethod(contract.MethodDiagnosticsExport) != contract.MethodDiagnosticsExport {
		t.Fatal("missing fixed export method")
	}
	for _, method := range []string{"diagnostics.export.private", "DIAGNOSTICS.EXPORT"} {
		if normalizeMethod(method) != methodUnknown {
			t.Fatal("untrusted export method accepted")
		}
	}
	var buf safeBuffer
	_, url := newTestServer(t, func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDiagnosticsExport {
			t.Fatal("wrong method")
		}
		return nil, &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "malformed diagnostic payload"}
	}, &buf)
	conn := dial(t, url)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "private-export-request", contract.MethodDiagnosticsExport, json.RawMessage(`{"password":"private-export-secret"}`))
	response := readResponse(t, conn)
	if response.OK || response.ID != "private-export-request" || response.Error.Code != contract.ErrorCodeInvalidArgument {
		t.Fatal("export response boundary changed")
	}
	waitForLogEvent(t, &buf, "method="+contract.MethodDiagnosticsExport)
	for _, secret := range []string{"private-export-request", "private-export-secret", "password", testToken} {
		if strings.Contains(buf.String(), secret) {
			t.Fatal("private export input leaked into log")
		}
	}
}
