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

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
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
	srv, h := serveTestRPC(t, handler, newTestLoggerWithLevel(buf, level), callback)
	return srv, testWebSocketURL(h.URL)
}

func serveTestRPC(t *testing.T, handler Handler, logger *slog.Logger, callback func(string)) (*Server, *httptest.Server) {
	t.Helper()
	srv, err := NewServer(testToken, testBuild, handler, logger, callback, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, serveTestHTTP(t, srv)
}

func serveTestHTTP(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	h := httptest.NewServer(handler)
	t.Cleanup(h.Close)
	return h
}

func testWebSocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ipc"
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

func serveTrackedTestHTTP(t *testing.T, handler http.Handler) (*httptest.Server, <-chan net.Conn) {
	t.Helper()
	h := httptest.NewUnstartedServer(handler)
	accepted := make(chan net.Conn, 1)
	h.Listener = &trackingListener{Listener: h.Listener, accepted: accepted}
	h.Start()
	t.Cleanup(h.Close)
	return h, accepted
}
