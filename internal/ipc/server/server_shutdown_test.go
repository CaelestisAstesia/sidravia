package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

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
	}, newTestLogger(&buf), nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handlerReturned := make(chan struct{})
	httpServer := serveTestHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r)
		close(handlerReturned)
	}))
	wsURL := testWebSocketURL(httpServer.URL)
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
