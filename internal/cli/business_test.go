package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestAuthBusinessReadOnlyAndTerminalSafe(t *testing.T) {
	raw := json.RawMessage(`{"sessionId":"s","runGeneration":1,"revision":2,"current":true,"statisticsSupported":false,"textSupported":true,"statistics":null,"messages":[{"id":"1","receivedAt":"2026-11-14T00:00:00Z","text":"hello\nworld"}],"updatedAt":"2026-11-14T00:00:00Z"}`)
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
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
	if connection.callCount != 1 || connection.closeCount != 1 || strings.Contains(output.String(), "hello\nworld") || !strings.Contains(output.String(), `hello\nworld`) {
		t.Fatalf("unsafe or incomplete output %q", output.String())
	}
}
