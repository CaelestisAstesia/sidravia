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
	"testing"
	"time"

	"sidravia/internal/ipc/contract"

	"github.com/coder/websocket"
)

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
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
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
	t.Helper()
	srv, err := NewServer(testToken, testBuild, handler, newTestLogger(buf))
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
			if _, err := NewServer(tc.token, tc.buildID, tc.handler, tc.logger); err == nil {
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
	srv, err := NewServer(testToken, testBuild, handler, newTestLogger(buf))
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
	srv, err := NewServer(testToken, testBuild, handler, newTestLogger(buf))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	t.Cleanup(httpServer.Close)
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	return srv, httpServer, wsURL
}
