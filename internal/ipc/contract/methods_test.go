package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func validStartJSON() []byte {
	return []byte(`{"displayName":"Library WiFi","institutionProfileId":"profile-1","username":"alice","password":"secret","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{"custom":"value"}}`)
}

func TestDecodeSessionStartOneShotPayloadAcceptsValid(t *testing.T) {
	payload, err := DecodeSessionStartOneShotPayload(validStartJSON())
	if err != nil {
		t.Fatalf("expected valid decode, got error: %v", err)
	}
	if payload.DisplayName != "Library WiFi" {
		t.Errorf("displayName: got %q, want %q", payload.DisplayName, "Library WiFi")
	}
	if payload.InstitutionProfileID != "profile-1" {
		t.Errorf("institutionProfileId: got %q, want %q", payload.InstitutionProfileID, "profile-1")
	}
	if payload.Username != "alice" {
		t.Errorf("username: got %q, want %q", payload.Username, "alice")
	}
	if payload.Password != "secret" {
		t.Errorf("password: got %q, want %q", payload.Password, "secret")
	}
	if payload.NetworkBindingPolicyMode != "automatically_select_latest_available" {
		t.Errorf("networkBindingPolicyMode: got %q", payload.NetworkBindingPolicyMode)
	}
	if string(payload.ProtocolContextOverride) != `{"custom":"value"}` {
		t.Errorf("protocolContextOverride: got %q", string(payload.ProtocolContextOverride))
	}
}

func TestDecodeSessionStartOneShotPayloadAcceptsEmptyPassword(t *testing.T) {
	data := []byte(`{"institutionProfileId":"profile-1","username":"alice","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{}}`)
	payload, err := DecodeSessionStartOneShotPayload(data)
	if err != nil {
		t.Fatalf("expected valid decode with empty password, got error: %v", err)
	}
	if payload.Password != "" {
		t.Errorf("expected empty password, got %q", payload.Password)
	}
}

func TestDecodeSessionStartOneShotPayloadRejectsEmptyUsername(t *testing.T) {
	data := []byte(`{"institutionProfileId":"profile-1","username":"","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{}}`)
	if _, err := DecodeSessionStartOneShotPayload(data); err == nil {
		t.Fatal("expected error for empty username, got nil")
	}
}

func TestDecodeSessionStartOneShotPayloadRejects(t *testing.T) {
	base := `{"institutionProfileId":"profile-1","username":"alice","password":"secret","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{}}`
	cases := []struct {
		name string
		data string
	}{
		{"unknown field", `{"displayName":"x","institutionProfileId":"profile-1","username":"alice","password":"secret","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{},"extra":"y"}`},
		{"missing username", `{"institutionProfileId":"profile-1","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{}}`},
		{"missing profile id", `{"username":"alice","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{}}`},
		{"missing binding mode", `{"institutionProfileId":"profile-1","username":"alice","protocolContextOverride":{}}`},
		{"missing override", `{"institutionProfileId":"profile-1","username":"alice","networkBindingPolicyMode":"automatically_select_latest_available"}`},
		{"null override", `{"institutionProfileId":"profile-1","username":"alice","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":null}`},
		{"null payload", `null`},
		{"empty payload", ``},
		{"trailing value", base + `{"second":1}`},
		{"trailing garbage", base + `!!!`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeSessionStartOneShotPayload([]byte(tc.data)); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestDecodeSessionStartOneShotPayloadDoesNotLeakPassword(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{
			"trailing garbage after password",
			`{"institutionProfileId":"p","username":"u","password":"SUPER-SECRET-PASSWORD-12345","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{}}!!!`,
		},
		{
			"malformed json around password",
			`{"institutionProfileId":"p","username":"u","password":"SUPER-SECRET-PASSWORD-12345","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{ INVALID}`,
		},
		{
			"unknown field with password",
			`{"institutionProfileId":"p","username":"u","password":"SUPER-SECRET-PASSWORD-12345","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{},"extra":"y"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeSessionStartOneShotPayload([]byte(tc.data))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if strings.Contains(err.Error(), "SUPER-SECRET-PASSWORD-12345") {
				t.Fatalf("decode error leaked password: %v", err)
			}
		})
	}
}

func TestDecodeSessionStopPayloadAcceptsValid(t *testing.T) {
	payload, err := DecodeSessionStopPayload([]byte(`{"sessionId":"sess-1"}`))
	if err != nil {
		t.Fatalf("expected valid decode, got error: %v", err)
	}
	if payload.SessionID != "sess-1" {
		t.Errorf("sessionId: got %q, want %q", payload.SessionID, "sess-1")
	}
}

func TestDecodeSessionStopPayloadRejects(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"unknown field", `{"sessionId":"sess-1","extra":"y"}`},
		{"missing session id", `{}`},
		{"empty session id", `{"sessionId":""}`},
		{"null payload", `null`},
		{"empty payload", ``},
		{"trailing value", `{"sessionId":"sess-1"}{"second":1}`},
		{"trailing garbage", `{"sessionId":"sess-1"}!!!`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeSessionStopPayload([]byte(tc.data)); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestDecodeSessionGetPayloadAcceptsValid(t *testing.T) {
	payload, err := DecodeSessionGetPayload([]byte(`{"sessionId":"sess-1"}`))
	if err != nil {
		t.Fatalf("expected valid decode, got error: %v", err)
	}
	if payload.SessionID != "sess-1" {
		t.Errorf("sessionId: got %q, want %q", payload.SessionID, "sess-1")
	}
}

func TestDecodeSessionGetPayloadRejects(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"unknown field", `{"sessionId":"sess-1","extra":"y"}`},
		{"missing session id", `{}`},
		{"empty session id", `{"sessionId":""}`},
		{"null payload", `null`},
		{"empty payload", ``},
		{"trailing value", `{"sessionId":"sess-1"}{"second":1}`},
		{"trailing garbage", `{"sessionId":"sess-1"}!!!`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeSessionGetPayload([]byte(tc.data)); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestMarshalSessionResultMapsAllFields(t *testing.T) {
	established := "2026-07-24T10:00:00Z"
	retry := "2026-07-24T10:05:00Z"
	result := SessionResult{
		AuthenticationSessionID:     "sess-1",
		DisplayName:                 "Library WiFi",
		InstitutionProfileID:        "profile-1",
		InstitutionDisplayName:      "Library",
		AuthenticationProtocolID:    "drcom",
		AccountLabel:                "public-account-label",
		Intent:                      "maintain_authentication",
		State:                       "authenticated",
		StateReason:                 &SessionStateReason{Code: "network_unavailable", Description: "no network"},
		SelectedNetworkBinding:      &SessionNetworkBinding{InterfaceID: "iface-1", DisplayName: "Eth0", LocalIPv4Address: "10.0.0.2"},
		AuthenticationEstablishedAt: &established,
		NextRetryAt:                 &retry,
		LastAuthenticationFailure:   &SessionAuthenticationFailure{Code: "credentials_rejected", Description: "bad credentials", HandlingRecommendation: "retry_after_standard_delay"},
		Revision:                    7,
		UpdatedAt:                   "2026-07-24T10:00:01Z",
	}
	data, err := MarshalSessionResult(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expected := map[string]any{
		"sessionId":                "sess-1",
		"displayName":              "Library WiFi",
		"institutionProfileId":     "profile-1",
		"institutionDisplayName":   "Library",
		"authenticationProtocolId": "drcom",
		"accountLabel":             "public-account-label",
		"intent":                   "maintain_authentication",
		"state":                    "authenticated",
		"revision":                 float64(7),
		"updatedAt":                "2026-07-24T10:00:01Z",
	}
	for key, want := range expected {
		got, ok := decoded[key]
		if !ok {
			t.Errorf("missing field %q in result JSON: %s", key, string(data))
			continue
		}
		if got != want {
			t.Errorf("field %q: got %v, want %v", key, got, want)
		}
	}
	for _, key := range []string{"stateReason", "selectedNetworkBinding", "lastAuthenticationFailure", "authenticationEstablishedAt", "nextRetryAt"} {
		if decoded[key] == nil {
			t.Errorf("optional field %q must be present when set: %s", key, string(data))
		}
	}
	nested := decoded["selectedNetworkBinding"].(map[string]any)
	if nested["localIpv4Address"] != "10.0.0.2" {
		t.Errorf("localIpv4Address: got %v", nested["localIpv4Address"])
	}
}

func TestMarshalSessionResultOmitsOptionalFields(t *testing.T) {
	result := SessionResult{
		AuthenticationSessionID: "sess-1",
		State:                   "suspended",
		Revision:                1,
		UpdatedAt:               "2026-07-24T10:00:00Z",
	}
	data, err := MarshalSessionResult(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"stateReason", "selectedNetworkBinding", "authenticationEstablishedAt", "nextRetryAt", "lastAuthenticationFailure"} {
		if _, present := decoded[key]; present {
			t.Errorf("optional field %q must be omitted when nil: %s", key, string(data))
		}
	}
}

func TestMarshalSessionResultHasNoSecretFields(t *testing.T) {
	result := SessionResult{
		AuthenticationSessionID: "sess-1",
		State:                   "suspended",
		Revision:                1,
		UpdatedAt:               "2026-07-24T10:00:00Z",
	}
	data, err := MarshalSessionResult(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"credentialId", "username", "password", "protocolContextOverride", "diagnosticCause"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("result JSON must not contain %q: %s", secret, string(data))
		}
	}
}
