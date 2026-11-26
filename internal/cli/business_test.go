package cli

import (
	"bytes"
	"encoding/json"
	"sidravia/internal/cli/internal/clitest"
	"strings"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestAuthBusinessReadOnlyAndTerminalSafe(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"s","runGeneration":1,"revision":2,"current":true,"statisticsSupported":false,"textSupported":true,"statistics":null,"messages":[{"id":"1","receivedAt":"2026-11-14T00:00:00Z","text":"hello\nworld"}],"updatedAt":"2026-11-14T00:00:00Z"}`)
	connection := &clitest.FakeDaemonClient{CallFunc: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodSessionBusinessGet || string(payload) != `{"sessionId":"s"}` {
			t.Fatal("business issued another operation")
		}
		return contract.Response{OK: true, Result: raw}, nil
	}}
	deps := hotAuthDependencies(t, connection)
	output := &bytes.Buffer{}
	deps.stdout = output
	if err := runAuthBusiness("s", deps); err != nil {
		t.Fatal(err)
	}
	if connection.CallCount != 1 || connection.CloseCount != 1 || strings.Contains(output.String(), "hello\nworld") || !strings.Contains(output.String(), `hello\nworld`) {
		t.Fatalf("unsafe or incomplete output %q", output.String())
	}
}
