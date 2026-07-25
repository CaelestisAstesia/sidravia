package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRequestValid(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status","payload":{}}`)
	req, err := DecodeRequest(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Kind != "request" {
		t.Errorf("kind: got %q", req.Kind)
	}
	if req.ID != "1" {
		t.Errorf("id: got %q", req.ID)
	}
	if req.Method != "daemon.status" {
		t.Errorf("method: got %q", req.Method)
	}
	if req.Payload == nil {
		t.Fatal("payload should not be nil")
	}
}

func TestDecodeRequestMissingPayload(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status"}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for missing payload")
	}
	if !strings.Contains(err.Error(), "missing payload") {
		t.Errorf("error should mention missing payload: %v", err)
	}
}

func TestDecodeRequestNullPayload(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status","payload":null}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for null payload")
	}
}

func TestDecodeRequestUnknownFields(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status","payload":{},"extra":"bad"}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for unknown fields")
	}
}

func TestDecodeRequestTrailingData(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status","payload":{}}garbage`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for trailing data")
	}
	if !strings.Contains(err.Error(), "trailing garbage") {
		t.Errorf("error should mention trailing garbage: %v", err)
	}
}

func TestDecodeRequestTrailingJSON(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status","payload":{}}{}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for trailing JSON")
	}
	if !strings.Contains(err.Error(), "trailing data") {
		t.Errorf("error should mention trailing data: %v", err)
	}
}

func TestDecodeRequestWrongKind(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","method":"daemon.status","payload":{}}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestDecodeRequestEmptyID(t *testing.T) {
	data := []byte(`{"kind":"request","id":"","method":"daemon.status","payload":{}}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestDecodeRequestEmptyMethod(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","method":"","payload":{}}`)
	_, err := DecodeRequest(data)
	if err == nil {
		t.Fatal("expected error for empty method")
	}
}

func TestDecodeResponseValidSuccess(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","ok":true,"result":{"productVersion":"0.1.0-dev","buildId":"dev","pid":123,"status":"running"}}`)
	resp, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok=true")
	}
	if resp.Error != nil {
		t.Fatal("expected no error")
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
}

func TestDecodeResponseValidError(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","ok":false,"error":{"code":"unknown_method","message":"unsupported method"}}`)
	resp, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok=false")
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != "unknown_method" {
		t.Errorf("error code: got %q", resp.Error.Code)
	}
}

func TestDecodeResponseOKTrueWithError(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","ok":true,"error":{"code":"x","message":"bad"}}`)
	_, err := DecodeResponse(data)
	if err == nil {
		t.Fatal("expected error for ok=true with error present")
	}
}

func TestDecodeResponseOKFalseWithoutError(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","ok":false}`)
	_, err := DecodeResponse(data)
	if err == nil {
		t.Fatal("expected error for ok=false without error")
	}
}

func TestDecodeResponseUnknownFields(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","ok":true,"result":{},"extra":"bad"}`)
	_, err := DecodeResponse(data)
	if err == nil {
		t.Fatal("expected error for unknown fields")
	}
}

func TestDecodeResponseTrailingData(t *testing.T) {
	data := []byte(`{"kind":"response","id":"1","ok":true,"result":{}}garbage`)
	_, err := DecodeResponse(data)
	if err == nil {
		t.Fatal("expected error for trailing data")
	}
	if !strings.Contains(err.Error(), "trailing garbage") {
		t.Errorf("error should mention trailing garbage: %v", err)
	}
}

func TestDecodeResponseWrongKind(t *testing.T) {
	data := []byte(`{"kind":"request","id":"1","ok":true,"result":{}}`)
	_, err := DecodeResponse(data)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestDecodeResponseEmptyID(t *testing.T) {
	data := []byte(`{"kind":"response","id":"","ok":true,"result":{}}`)
	resp, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("unexpected error for empty id: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok=true")
	}
}

func TestEncodeResponseSuccess(t *testing.T) {
	resp := NewSuccessResponse("1", json.RawMessage(`{"status":"running"}`))
	data, err := EncodeResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !decoded.OK {
		t.Fatal("expected ok=true")
	}
	if string(decoded.Result) != `{"status":"running"}` {
		t.Errorf("result: got %s", string(decoded.Result))
	}
}

func TestEncodeResponseError(t *testing.T) {
	resp := NewErrorResponse("1", "unknown_method", "unsupported method")
	data, err := EncodeResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.OK {
		t.Fatal("expected ok=false")
	}
	if decoded.Error.Code != "unknown_method" {
		t.Errorf("error code: got %q", decoded.Error.Code)
	}
}
