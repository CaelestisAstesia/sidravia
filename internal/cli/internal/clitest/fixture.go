// Package clitest supplies shared CLI test fixtures.
package clitest

import (
	"context"
	"encoding/json"
	"testing"

	"sidravia/internal/ipc/contract"
)

type FakeDaemonClient struct {
	CallCount   int
	CloseCount  int
	CallFunc    func(string, json.RawMessage) (contract.Response, error)
	CallContext func(context.Context)
	CloseErr    error
}

func (client *FakeDaemonClient) Call(
	ctx context.Context,
	method string,
	payload json.RawMessage,
) (contract.Response, error) {
	client.CallCount++
	if client.CallContext != nil {
		client.CallContext(ctx)
	}
	return client.CallFunc(method, payload)
}

func (client *FakeDaemonClient) Close() error {
	client.CloseCount++
	return client.CloseErr
}

func MinimalSessionResult(state string) contract.SessionResult {
	return contract.SessionResult{
		ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"},
		Revision:       1, Intent: "maintain_authentication", InstitutionDisplayName: "Profile",
		AuthenticationSessionID:  "session-1",
		InstitutionProfileID:     "profile-1",
		AuthenticationProtocolID: "protocol-1",
		AccountName:              "account-name",
		State:                    state,
		UpdatedAt:                "2026-07-26T10:11:12.123456789+08:00",
	}
}

func SuccessSessionResponse(t *testing.T, result contract.SessionResult) contract.Response {
	t.Helper()
	payload, err := contract.MarshalSessionResult(result)
	if err != nil {
		t.Fatalf("MarshalSessionResult = %v", err)
	}
	return contract.NewSuccessResponse("1", payload)
}

func SuccessSessionStartResponse(t *testing.T, outcome string, result contract.SessionResult) contract.Response {
	t.Helper()
	payload, err := contract.MarshalSessionStartResult(contract.SessionStartResult{Outcome: outcome, Session: result})
	if err != nil {
		t.Fatalf("MarshalSessionStartResult = %v", err)
	}
	return contract.NewSuccessResponse("1", payload)
}

func ErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type ZeroWriter struct {
	Err error
}

func (writer ZeroWriter) Write([]byte) (int, error) {
	return 0, writer.Err
}
