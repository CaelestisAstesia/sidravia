package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/ipc/server"
)

func TestStatusHandlerSuccess(t *testing.T) {
	handler := StatusHandler("0.1.0-dev", "dev")
	srv := server.NewServer("test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev", handler)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	c, err := client.Connect(context.Background(), wsURL, "test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	resp, err := c.Call(context.Background(), contract.MethodDaemonStatus, nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}

	var result contract.StatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ProductVersion != "0.1.0-dev" {
		t.Errorf("product version: got %q, want %q", result.ProductVersion, "0.1.0-dev")
	}
	if result.BuildID != "dev" {
		t.Errorf("build ID: got %q, want %q", result.BuildID, "dev")
	}
	if result.PID <= 0 {
		t.Errorf("pid must be positive, got %d", result.PID)
	}
	if result.Status != "running" {
		t.Errorf("status: got %q, want %q", result.Status, "running")
	}
}

func TestStatusHandlerUnknownMethod(t *testing.T) {
	handler := StatusHandler("0.1.0-dev", "dev")
	srv := server.NewServer("test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev", handler)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	c, err := client.Connect(context.Background(), wsURL, "test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	resp, err := c.Call(context.Background(), "unknown.method", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK {
		t.Fatal("expected error response")
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != contract.ErrorCodeUnknownMethod {
		t.Errorf("error code: got %q, want %q", resp.Error.Code, contract.ErrorCodeUnknownMethod)
	}
}

func TestStatusHandlerMalformedRequest(t *testing.T) {
	handler := StatusHandler("0.1.0-dev", "dev")
	srv := server.NewServer("test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev", handler)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	header := http.Header{}
	header.Set("Authorization", "Bearer test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab")
	header.Set("Sidravia-Build-ID", "dev")
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	badData := []byte(`{"kind":"bad","id":"1"}`)
	if err := conn.Write(context.Background(), websocket.MessageText, badData); err != nil {
		t.Fatalf("write bad request: %v", err)
	}
	_, respData, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp, err := contract.DecodeResponse(respData)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected error response")
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != contract.ErrorCodeMalformed {
		t.Errorf("error code: got %q, want %q", resp.Error.Code, contract.ErrorCodeMalformed)
	}
}

func TestStatusHandlerAuthTokenRejected(t *testing.T) {
	handler := StatusHandler("0.1.0-dev", "dev")
	srv := server.NewServer("correct-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev", handler)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	_, err := client.Connect(context.Background(), wsURL, "wrong-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev")
	if err == nil {
		t.Fatal("expected connect error for wrong token")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got: %v", err)
	}
}

func TestStatusHandlerBuildIDRejected(t *testing.T) {
	handler := StatusHandler("0.1.0-dev", "dev")
	srv := server.NewServer("test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev", handler)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	_, err := client.Connect(context.Background(), wsURL, "test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "wrong-build")
	if err == nil {
		t.Fatal("expected connect error for wrong build ID")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("expected 409 error, got: %v", err)
	}
}

func TestStatusHandlerSequentialRequests(t *testing.T) {
	handler := StatusHandler("0.1.0-dev", "dev")
	srv := server.NewServer("test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev", handler)

	httpServer := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	c, err := client.Connect(context.Background(), wsURL, "test-token-0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab", "dev")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	for i := 0; i < 5; i++ {
		resp, err := c.Call(context.Background(), contract.MethodDaemonStatus, nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if !resp.OK {
			t.Fatalf("call %d: unexpected error %+v", i, resp.Error)
		}
		if resp.ID != "1" {
			t.Errorf("call %d: response id mismatch", i)
		}
	}
}
