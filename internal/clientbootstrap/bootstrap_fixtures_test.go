package clientbootstrap

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

type fakeClient struct {
	call  func(string, json.RawMessage) (contract.Response, error)
	close int
}

func (c *fakeClient) Call(_ context.Context, method string, payload json.RawMessage) (contract.Response, error) {
	return c.call(method, payload)
}
func (c *fakeClient) Close() error { c.close++; return nil }

func runtime(pid int) contract.RuntimeInfo {
	return contract.RuntimeInfo{Endpoint: "ws://127.0.0.1:1", Token: "token", ProductVersion: "1.0.0", BuildID: "build", PID: pid}
}

func testIdentity(t *testing.T) Identity {
	t.Helper()
	identity, err := NewIdentity("1.0.0", "build")
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func statusResponse(t *testing.T) contract.Response {
	return statusResponseFor(t, "headless", nil)
}

func statusResponseFor(t *testing.T, mode string, owner *int) contract.Response {
	t.Helper()
	return statusResponseValue(t, contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: 1, Status: "running", Mode: mode, DesktopOwnerPID: owner})
}

func statusResponseValue(t *testing.T, value contract.StatusResult) contract.Response {
	t.Helper()
	b, err := contract.MarshalStatusResult(value)
	if err != nil {
		t.Fatal(err)
	}
	return contract.NewSuccessResponse("1", b)
}

func intPtr(value int) *int { return &value }

func mustDesktop(t *testing.T, owner int) launchcontract.Options {
	t.Helper()
	options, err := launchcontract.Desktop(owner)
	if err != nil {
		t.Fatal(err)
	}
	return options
}

func testDependencies(read func(string) (contract.RuntimeInfo, error), connect func(context.Context, contract.RuntimeInfo, Identity) (Client, error)) dependencies {
	return dependencies{
		runtimeInfoPath: func(productlayout.Namespace) (string, error) { return "runtime", nil },
		readRuntimeInfo: read,
		connect:         connect,
		launch:          func(launchcontract.Options, string) (daemonLaunch, error) { return daemonLaunch{}, nil },
		namespace:       func() (productlayout.Namespace, error) { return productlayout.Namespace{}, nil },
		callTimeout:     time.Second, totalWait: time.Second, pollInterval: time.Millisecond,
	}
}

func statusClient(t *testing.T, mode string, owner *int) *fakeClient {
	return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
		return statusResponseFor(t, mode, owner), nil
	}}
}
