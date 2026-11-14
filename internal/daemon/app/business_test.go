package app

import (
	"context"
	"encoding/json"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/ipc/contract"
	"testing"
	"time"
)

type readOnlyBusinessApp struct {
	calls int
	id    session.AuthenticationSessionID
}

func (a *readOnlyBusinessApp) GetSessionBusiness(_ context.Context, id session.AuthenticationSessionID) (session.BusinessSnapshot, error) {
	a.calls++
	a.id = id
	return session.BusinessSnapshot{SessionID: id, Revision: 1, Messages: []session.BusinessMessage{}, UpdatedAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)}, nil
}
func TestBusinessHandlerReadOnlyAndStrict(t *testing.T) {
	a := &readOnlyBusinessApp{}
	h := BusinessHandler(a)
	data, failure := h(context.Background(), contract.MethodSessionBusinessGet, json.RawMessage(`{"sessionId":"s"}`))
	if failure != nil {
		t.Fatal(failure)
	}
	v, err := contract.DecodeBusinessResult(data)
	if err != nil || a.calls != 1 || a.id != "s" || v.StatisticsSupported || v.TextSupported || v.Current || v.Statistics != nil {
		t.Fatalf("result %#v %v", v, err)
	}
	for _, payload := range []string{`{}`, `{"sessionId":"s","probe":true}`, `{"sessionId":"s","sessionId":"other"}`, `{"sessionId":null}`} {
		_, failure = h(context.Background(), contract.MethodSessionBusinessGet, json.RawMessage(payload))
		if failure == nil || failure.Code != contract.ErrorCodeInvalidArgument {
			t.Fatalf("accepted %s", payload)
		}
	}
	if a.calls != 1 {
		t.Fatal("invalid request reached application")
	}
	_, failure = h(context.Background(), contract.MethodSessionRestart, json.RawMessage(`{"sessionId":"s"}`))
	if failure == nil || failure.Code != contract.ErrorCodeUnknownMethod || a.calls != 1 {
		t.Fatal("business handler admitted mutation")
	}
}
