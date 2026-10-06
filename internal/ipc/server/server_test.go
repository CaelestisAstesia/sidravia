package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"

	"github.com/coder/websocket"
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

const (
	testToken = "test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
	testBuild = "dev"
)

// safeBuffer is a concurrency-safe bytes.Buffer so the slog TextHandler
// (writing from the server goroutine) and the test (reading assertions) never
// race on the captured log output.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

type trackingListener struct {
	net.Listener
	accepted chan net.Conn
}

func (l *trackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.accepted <- conn
	return conn, nil
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestLogger(buf *safeBuffer) *slog.Logger {
	return newTestLoggerWithLevel(buf, slog.LevelDebug)
}

func newTestLoggerWithLevel(buf *safeBuffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}))
}

func waitForLogEvent(t *testing.T, buf *safeBuffer, needle string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), needle) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log %q, got:\n%s", needle, buf.String())
}

func waitForChannel(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func newTestServer(t *testing.T, handler Handler, buf *safeBuffer) (*Server, string) {
	return newTestServerWithCallback(t, handler, buf, slog.LevelDebug, nil)
}

func newTestServerWithLevel(t *testing.T, handler Handler, buf *safeBuffer, level slog.Level) (*Server, string) {
	return newTestServerWithCallback(t, handler, buf, level, nil)
}

func newTestServerWithCallback(t *testing.T, handler Handler, buf *safeBuffer, level slog.Level, callback func(string)) (*Server, string) {
	t.Helper()
	srv, err := NewServer(testToken, testBuild, handler, newTestLoggerWithLevel(buf, level), callback)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	t.Cleanup(httpServer.Close)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	return srv, wsURL
}

func dial(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+testToken)
	header.Set("Sidravia-Build-ID", testBuild)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func writeRequest(t *testing.T, conn *websocket.Conn, id, method string, payload json.RawMessage) {
	t.Helper()
	req := contract.Request{
		Kind:    string(contract.KindRequest),
		ID:      id,
		Method:  method,
		Payload: payload,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func readResponse(t *testing.T, conn *websocket.Conn) contract.Response {
	t.Helper()
	_, data, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp, err := contract.DecodeResponse(data)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestNewServerRejectsNilDependencies(t *testing.T) {
	handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) { return nil, nil }
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	for _, tc := range []struct {
		name    string
		token   string
		buildID string
		handler Handler
		logger  *slog.Logger
	}{
		{"empty token", "", testBuild, handler, logger},
		{"empty build id", testToken, "", handler, logger},
		{"nil handler", testToken, testBuild, nil, logger},
		{"nil logger", testToken, testBuild, handler, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewServer(tc.token, tc.buildID, tc.handler, tc.logger, nil); err == nil {
				t.Fatal("NewServer expected error for nil/empty dependency")
			}
		})
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

func TestIPCErrorResponseEncodesAndPreservesRequestID(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		return nil, &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "invalid argument"}
	}
	_, wsURL := newTestServer(t, handler, &buf)
	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "error-request-id", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	response := readResponse(t, conn)
	if response.ID != "error-request-id" || response.OK || response.Error == nil || response.Error.Code != contract.ErrorCodeInvalidArgument || response.Error.Message != "invalid argument" {
		t.Fatalf("error response = %#v", response)
	}
	waitForLogEvent(t, &buf, "event=ipc_request_rejected")
	if strings.Contains(buf.String(), "event=ipc_response_failed") {
		t.Fatalf("normal error response unexpectedly failed encoding: %s", buf.String())
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
	wsURL, accepted := newTrackedTestServer(t, handler, &buf)

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
	_, httpServer, _ := newTestServerWithHTTP(t, handler, &buf)

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

func TestExistingIPCResponsesUnchanged(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method == contract.MethodDaemonStatus {
			return json.RawMessage(`{"productVersion":"1.0.0","buildId":"b","pid":1,"status":"running"}`), nil
		}
		return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported"}
	}
	_, wsURL := newTestServer(t, handler, &buf)

	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	resp := readResponse(t, conn)
	if !resp.OK {
		t.Fatalf("success response expected OK, got error: %+v", resp.Error)
	}
	if resp.ID != "1" {
		t.Fatalf("success response id = %q, want 1", resp.ID)
	}

	writeRequest(t, conn, "2", "unknown.method", json.RawMessage(`{}`))
	resp = readResponse(t, conn)
	if resp.OK {
		t.Fatal("unknown method response expected error")
	}
	if resp.Error.Code != contract.ErrorCodeUnknownMethod {
		t.Fatalf("unknown method error code = %q, want %q", resp.Error.Code, contract.ErrorCodeUnknownMethod)
	}
	if resp.ID != "2" {
		t.Fatalf("error response id = %q, want 2", resp.ID)
	}
}

func newTrackedTestServer(t *testing.T, handler Handler, buf *safeBuffer) (string, <-chan net.Conn) {
	t.Helper()
	srv, err := NewServer(testToken, testBuild, handler, newTestLogger(buf), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(srv.ServeHTTP))
	accepted := make(chan net.Conn, 1)
	httpServer.Listener = &trackingListener{Listener: httpServer.Listener, accepted: accepted}
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	return wsURL, accepted
}

// newTestServerWithHTTP returns the raw httptest.Server (http URL) in addition
// to the websocket URL, for tests that drive the HTTP layer directly.
func newTestServerWithHTTP(t *testing.T, handler Handler, buf *safeBuffer) (*Server, *httptest.Server, string) {
	t.Helper()
	srv, err := NewServer(testToken, testBuild, handler, newTestLogger(buf), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	t.Cleanup(httpServer.Close)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	return srv, httpServer, wsURL
}

// TestResponseCommittedFiresAfterSuccessWrite proves the committed callback
// fires only after a success response is written, with the raw request method.
func TestResponseCommittedFiresAfterSuccessWrite(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		return json.RawMessage(`{}`), nil
	}
	var mu sync.Mutex
	committed := []string{}
	_, wsURL := newTestServerWithCallback(t, handler, &buf, slog.LevelDebug, func(method string) {
		mu.Lock()
		defer mu.Unlock()
		committed = append(committed, method)
	})
	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	readResponse(t, conn)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(committed)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(committed) != 1 || committed[0] != contract.MethodDaemonStatus {
		t.Errorf("committed = %v, want [daemon.status]", committed)
	}
}

// TestResponseCommittedNotFiredOnRejectedRequest proves a rejected request
// (handler error) never fires the committed callback, since the response is
// not a success.
func TestResponseCommittedNotFiredOnRejectedRequest(t *testing.T) {
	var buf safeBuffer
	handler := func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		return nil, &contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "fail"}
	}
	var mu sync.Mutex
	fired := false
	_, wsURL := newTestServerWithCallback(t, handler, &buf, slog.LevelDebug, func(string) {
		mu.Lock()
		defer mu.Unlock()
		fired = true
	})
	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "1", contract.MethodSessionGet, json.RawMessage(`{"sessionId":"s1"}`))
	readResponse(t, conn)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := fired
		mu.Unlock()
		if got {
			t.Fatal("responseCommitted fired for rejected request")
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if fired {
		t.Fatal("responseCommitted must not fire for rejected request")
	}
}

func TestShutdownClosesRegisteredConnectionsAndWaitsForHandler(t *testing.T) {
	var buf safeBuffer
	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	handlerRelease := make(chan struct{})
	handler := func(ctx context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDaemonStatus {
			return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported"}
		}
		close(handlerStarted)
		<-ctx.Done()
		close(handlerCanceled)
		<-handlerRelease
		return json.RawMessage(`{}`), nil
	}
	srv, wsURL := newTestServer(t, handler, &buf)
	conn := dial(t, wsURL)
	defer conn.Close(websocket.StatusNormalClosure, "")
	writeRequest(t, conn, "1", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	waitForChannel(t, handlerStarted, "handler start")

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- srv.Shutdown(context.Background()) }()
	waitForChannel(t, handlerCanceled, "handler cancellation")
	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned before handler exit: %v", err)
	default:
	}
	close(handlerRelease)
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return after handler exit")
	}

	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := conn.Read(readCtx); err == nil {
		t.Fatal("connection remained readable after shutdown")
	}
}

func TestShutdownAfterNormalClientCloseReturns(t *testing.T) {
	var buf safeBuffer
	srv, wsURL := newTestServer(t, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		return json.RawMessage(`{}`), nil
	}, &buf)
	conn := dial(t, wsURL)
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("client Close: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		err := srv.Shutdown(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Shutdown did not return after normal client close")
}

func TestShutdownClosesAdmissionGateBeforeLateUpgrade(t *testing.T) {
	var buf safeBuffer
	var handlerCalls atomic.Int32
	srv, err := NewServer(testToken, testBuild, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		handlerCalls.Add(1)
		return json.RawMessage(`{}`), nil
	}, newTestLogger(&buf), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handlerReturned := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r)
		close(handlerReturned)
	}))
	t.Cleanup(httpServer.Close)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization":     []string{"Bearer " + testToken},
		"Sidravia-Build-ID": []string{testBuild},
	}})
	if err != nil {
		t.Fatalf("late authenticated upgrade: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-handlerReturned:
	case <-time.After(time.Second):
		t.Fatal("HTTP handler did not return after rejecting late upgrade")
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("late connection remained open after shutdown gate closed")
	}
	if got := handlerCalls.Load(); got != 0 {
		t.Fatalf("late upgrade entered handler %d times", got)
	}
}
