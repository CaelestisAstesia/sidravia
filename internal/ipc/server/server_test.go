package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

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
			if _, err := NewServer(tc.token, tc.buildID, tc.handler, tc.logger, nil, nil); err == nil {
				t.Fatal("NewServer expected error for nil/empty dependency")
			}
		})
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

func TestIPCRejectsAmbiguous(t *testing.T) {
	for _, data := range []string{
		`{"kind":"request","id":"1","method":"daemon.status","Method":"daemon.stop","payload":{}}`,
		`{"kind":"request","id":"1","method":"daemon.status","payload":{"password":"PRIVATE-SECRET-\ud800"}}`,
	} {
		t.Run("malformed", func(t *testing.T) {
			var buf safeBuffer
			var calls atomic.Int32
			handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
				calls.Add(1)
				return json.RawMessage(`{}`), nil
			}
			_, url := newTestServer(t, handler, &buf)
			conn := dial(t, url)
			defer conn.Close(websocket.StatusNormalClosure, "")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := conn.Write(ctx, websocket.MessageText, []byte(data)); e != nil {
				t.Fatal(e)
			}
			resp := readResponse(t, conn)
			if resp.OK || resp.Error == nil || resp.Error.Code != contract.ErrorCodeMalformed || calls.Load() != 0 {
				t.Fatalf("malformed dispatched: calls=%d error=%#v", calls.Load(), resp.Error)
			}
			waitForLogEvent(t, &buf, "event=ipc_request_rejected")
			if strings.Contains(buf.String(), "PRIVATE-SECRET") {
				t.Fatal("secret leaked to log")
			}
		})
	}
}
