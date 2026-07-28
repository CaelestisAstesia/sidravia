package app

import (
	"context"
	"encoding/json"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestDaemonHandlerStopReturnsStopping(t *testing.T) {
	handler := DaemonHandler()
	result, cerr := handler(context.Background(), contract.MethodDaemonStop, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("daemon.stop error: %+v", cerr)
	}
	var stopResult contract.DaemonStopResult
	if err := json.Unmarshal(result, &stopResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stopResult.Status != "stopping" {
		t.Errorf("status = %q, want stopping", stopResult.Status)
	}
	// The result must be exactly {"status":"stopping"}.
	if string(result) != `{"status":"stopping"}` {
		t.Errorf("result = %s, want {\"status\":\"stopping\"}", result)
	}
}

func TestDaemonHandlerStopRejectsNonCanonicalPayload(t *testing.T) {
	handler := DaemonHandler()
	for _, payload := range []string{"null", "", `{"extra":"x"}`, `{} {}`, `[]`} {
		_, cerr := handler(context.Background(), contract.MethodDaemonStop, []byte(payload))
		if cerr == nil {
			t.Errorf("payload %q expected error", payload)
			continue
		}
		if cerr.Code != contract.ErrorCodeInvalidArgument {
			t.Errorf("payload %q code = %q, want invalid_argument", payload, cerr.Code)
		}
	}
}

func TestDaemonHandlerUnknownMethod(t *testing.T) {
	handler := DaemonHandler()
	_, cerr := handler(context.Background(), "daemon.unknown", []byte(`{}`))
	if cerr == nil {
		t.Fatal("expected error for unknown daemon method")
	}
	if cerr.Code != contract.ErrorCodeUnknownMethod {
		t.Errorf("code = %q, want unknown_method", cerr.Code)
	}
}
