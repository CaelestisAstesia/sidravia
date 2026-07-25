package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRuntimeInfoValid(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`)
	info, err := DecodeRuntimeInfo(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.SchemaVersion != 1 {
		t.Errorf("schema version: got %d, want 1", info.SchemaVersion)
	}
	if info.Endpoint != "ws://127.0.0.1:12345/ipc" {
		t.Errorf("endpoint: got %q", info.Endpoint)
	}
	if info.PID != 123 {
		t.Errorf("pid: got %d, want 123", info.PID)
	}
	if info.Token != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("token mismatch")
	}
	if info.ProductVersion != "0.1.0-dev" {
		t.Errorf("product version: got %q", info.ProductVersion)
	}
	if info.BuildID != "dev" {
		t.Errorf("build id: got %q", info.BuildID)
	}
}

func TestDecodeRuntimeInfoUnknownFields(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev","extra":"bad"}`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for unknown fields")
	}
}

func TestDecodeRuntimeInfoTrailingData(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}garbage`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for trailing data")
	}
	if !strings.Contains(err.Error(), "trailing garbage") {
		t.Errorf("error should mention trailing garbage: %v", err)
	}
}

func TestDecodeRuntimeInfoTrailingJSON(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}{}`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for trailing JSON")
	}
	if !strings.Contains(err.Error(), "trailing data") {
		t.Errorf("error should mention trailing data: %v", err)
	}
}

func TestDecodeRuntimeInfoInvalidJSON(t *testing.T) {
	_, err := DecodeRuntimeInfo([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecodeRuntimeInfoMissingFields(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"missing endpoint", `{"schemaVersion":1,"pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`},
		{"missing pid", `{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`},
		{"missing token", `{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"productVersion":"0.1.0-dev","buildId":"dev"}`},
		{"missing productVersion", `{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","buildId":"dev"}`},
		{"missing buildId", `{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeRuntimeInfo([]byte(tt.json))
			if err == nil {
				t.Fatal("expected error for missing field")
			}
		})
	}
}

func TestDecodeRuntimeInfoBadSchemaVersion(t *testing.T) {
	data := []byte(`{"schemaVersion":99,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
}

func TestDecodeRuntimeInfoNonLoopback(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://192.168.1.1:12345/ipc","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for non-loopback endpoint")
	}
}

func TestDecodeRuntimeInfoWrongPath(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/wrong","pid":123,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for wrong path")
	}
}

func TestDecodeRuntimeInfoBadToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"too short", "abc"},
		{"too long", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef00"},
		{"uppercase", "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"},
		{"non-hex", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdeg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":123,"token":"` + tt.token + `","productVersion":"0.1.0-dev","buildId":"dev"}`)
			_, err := DecodeRuntimeInfo(data)
			if err == nil {
				t.Fatal("expected error for bad token")
			}
		})
	}
}

func TestDecodeRuntimeInfoNonPositivePID(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"endpoint":"ws://127.0.0.1:12345/ipc","pid":0,"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","productVersion":"0.1.0-dev","buildId":"dev"}`)
	_, err := DecodeRuntimeInfo(data)
	if err == nil {
		t.Fatal("expected error for non-positive PID")
	}
}

func TestEncodeRuntimeInfoJSONFields(t *testing.T) {
	info := RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:12345/ipc",
		PID:            123,
		Token:          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ProductVersion: "0.1.0-dev",
		BuildID:        "dev",
	}
	data, err := EncodeRuntimeInfo(info)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectedFields := map[string]bool{
		"schemaVersion":  true,
		"endpoint":       true,
		"pid":            true,
		"token":          true,
		"productVersion": true,
		"buildId":        true,
	}
	for key := range raw {
		if !expectedFields[key] {
			t.Errorf("unexpected JSON field %q (should be camelCase)", key)
		}
	}
	for key := range expectedFields {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing JSON field %q", key)
		}
	}
}

func TestRuntimeInfoEncodeDecodeRoundtrip(t *testing.T) {
	info := RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:12345/ipc",
		PID:            123,
		Token:          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ProductVersion: "0.1.0-dev",
		BuildID:        "dev",
	}
	data, err := EncodeRuntimeInfo(info)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeRuntimeInfo(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != info {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", decoded, info)
	}
}

func TestRuntimeInfoSafeDisplayNoToken(t *testing.T) {
	info := RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:12345/ipc",
		PID:            123,
		Token:          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ProductVersion: "0.1.0-dev",
		BuildID:        "dev",
	}
	display := info.SafeDisplay()
	if strings.Contains(display, info.Token) {
		t.Fatal("SafeDisplay must not contain token")
	}
	if strings.Contains(display, "0123456789abcdef") {
		t.Fatal("SafeDisplay must not contain any part of token")
	}
}
