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
		AccountName:                 "public-account-label",
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
		"accountName":              "public-account-label",
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

func TestDecodeEmptyPayloadStrict(t *testing.T) {
	if err := DecodeEmptyPayload([]byte(`{}`)); err != nil {
		t.Fatalf("DecodeEmptyPayload({}) = %v", err)
	}
	for _, data := range []string{
		"",
		"null",
		"[]",
		`{"extra":true}`,
		`{} {}`,
		`{} trailing`,
	} {
		if err := DecodeEmptyPayload([]byte(data)); err == nil {
			t.Errorf("DecodeEmptyPayload(%q) = nil, want error", data)
		}
	}
}

func TestMarshalListResultsUseNonNullArrays(t *testing.T) {
	sessionData, err := MarshalSessionListResult(SessionListResult{})
	if err != nil {
		t.Fatalf("MarshalSessionListResult() error = %v", err)
	}
	if string(sessionData) != `{"sessions":[]}` {
		t.Errorf("empty Session list = %s", sessionData)
	}

	profileData, err := MarshalProfileListResult(ProfileListResult{})
	if err != nil {
		t.Fatalf("MarshalProfileListResult() error = %v", err)
	}
	if string(profileData) != `{"profiles":[]}` {
		t.Errorf("empty Profile list = %s", profileData)
	}
}

func TestMarshalProfileListResultContainsOnlySafeSummaryFields(t *testing.T) {
	data, err := MarshalProfileListResult(ProfileListResult{
		Profiles: []ProfileSummaryResult{{
			InstitutionProfileID:     "jlu",
			DisplayName:              "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
		}},
	})
	if err != nil {
		t.Fatalf("MarshalProfileListResult() error = %v", err)
	}
	var decoded map[string][]map[string]string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal Profile list: %v", err)
	}
	profile := decoded["profiles"][0]
	if len(profile) != 3 ||
		profile["institutionProfileId"] != "jlu" ||
		profile["displayName"] != "吉林大学" ||
		profile["authenticationProtocolId"] != "drcom-5.2.0-d" {
		t.Errorf("Profile summary = %#v", profile)
	}
	for _, secret := range []string{"password", "credentialId", "institutionProtocolConfiguration"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("Profile list contains %q: %s", secret, data)
		}
	}
}

func TestMarshalDaemonStopResultIsStopping(t *testing.T) {
	data, err := MarshalDaemonStopResult(DaemonStopResult{Status: "stopping"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"status":"stopping"}` {
		t.Errorf("daemon stop result = %s, want stopping status", data)
	}
}

func TestRetainedSessionPayloadsAreStrict(t *testing.T) {
	decoders := []struct {
		name   string
		decode func([]byte) (string, error)
	}{
		{"ensure", func(data []byte) (string, error) {
			got, err := DecodeSessionEnsureRunningPayload(data)
			return got.SessionID, err
		}},
		{"restart", func(data []byte) (string, error) {
			got, err := DecodeSessionRestartPayload(data)
			return got.SessionID, err
		}},
		{"remove", func(data []byte) (string, error) {
			got, err := DecodeSessionRemovePayload(data)
			return got.SessionID, err
		}},
	}
	invalid := []string{"", "null", `{}`, `{"sessionId":""}`, `{"sessionId":"session-1","extra":true}`, `{"sessionId":"session-1"}{}`, `{"sessionId":"session-1"}!`}
	for _, decoder := range decoders {
		t.Run(decoder.name, func(t *testing.T) {
			if id, err := decoder.decode([]byte(`{"sessionId":"session-1"}`)); err != nil || id != "session-1" {
				t.Fatalf("valid payload id=%q err=%v", id, err)
			}
			for _, data := range invalid {
				if _, err := decoder.decode([]byte(data)); err == nil {
					t.Fatalf("accepted invalid payload %q", data)
				}
			}
		})
	}
	data, err := MarshalSessionRemoveResult(SessionRemoveResult{SessionID: "session-1", Status: "removed"})
	if err != nil || string(data) != `{"sessionId":"session-1","status":"removed"}` {
		t.Fatalf("remove result = %s, %v", data, err)
	}
}

func TestConfigurationPayloadDecodersAreStrict(t *testing.T) {
	type decoderCase struct {
		name    string
		valid   string
		decode  func([]byte) error
		invalid []string
	}
	cases := []decoderCase{
		{
			name:  "id",
			valid: `{"configurationId":"campus"}`,
			decode: func(data []byte) error {
				_, err := DecodeConfigurationIDPayload(data)
				return err
			},
			invalid: []string{"", "null", `{}`, `{"configurationId":""}`, `{"configurationId":"campus","extra":true}`},
		},
		{
			name:  "create",
			valid: `{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","password":"","allowInsecureStorage":false,"autoLogin":false,"autoReconnect":false}`,
			decode: func(data []byte) error {
				_, err := DecodeConfigurationCreatePayload(data)
				return err
			},
			invalid: []string{
				"", "null", `{}`,
				`{"configurationId":"campus","institutionProfileId":"jlu","username":"user","password":"","allowInsecureStorage":false}`,
				`{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","allowInsecureStorage":false}`,
				`{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","password":""}`,
				`{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"","password":"","allowInsecureStorage":false}`,
				`{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","password":"","allowInsecureStorage":false,"extra":true}`,
				`{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","password":"","allowInsecureStorage":false,"autoLogin":false}`,
				`{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","password":"","allowInsecureStorage":false,"autoReconnect":false}`,
			},
		},
		{
			name:  "update",
			valid: `{"configurationId":"campus","displayName":""}`,
			decode: func(data []byte) error {
				_, err := DecodeConfigurationUpdatePayload(data)
				return err
			},
			invalid: []string{"", "null", `{}`, `{"configurationId":"campus"}`, `{"configurationId":"campus","institutionProfileId":""}`, `{"configurationId":"campus","username":""}`, `{"configurationId":"campus","displayName":"","extra":true}`},
		},
		{
			name:  "set-password",
			valid: `{"configurationId":"campus","password":"","allowInsecureStorage":false}`,
			decode: func(data []byte) error {
				_, err := DecodeConfigurationSetPasswordPayload(data)
				return err
			},
			invalid: []string{"", "null", `{}`, `{"configurationId":"campus","allowInsecureStorage":false}`, `{"configurationId":"campus","password":""}`, `{"configurationId":"campus","password":"","allowInsecureStorage":false,"extra":true}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.decode([]byte(tc.valid)); err != nil {
				t.Fatalf("valid payload rejected: %v", err)
			}
			for _, invalid := range tc.invalid {
				for _, data := range []string{invalid, invalid + `{}`, invalid + `!`} {
					if err := tc.decode([]byte(data)); err == nil {
						t.Fatalf("accepted invalid payload %q", data)
					} else if strings.Contains(err.Error(), "private-password-marker") {
						t.Fatalf("decoder leaked payload: %v", err)
					}
				}
			}
		})
	}
}

func TestConfigurationUpdateDistinguishesAbsentAndExplicitEmpty(t *testing.T) {
	display, err := DecodeConfigurationUpdatePayload([]byte(`{"configurationId":"campus","displayName":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if display.DisplayName == nil || *display.DisplayName != "" || display.Username != nil || display.InstitutionProfileID != nil {
		t.Fatalf("display update = %#v", display)
	}
	username, err := DecodeConfigurationUpdatePayload([]byte(`{"configurationId":"campus","username":"user"}`))
	if err != nil {
		t.Fatal(err)
	}
	if username.Username == nil || *username.Username != "user" || username.DisplayName != nil {
		t.Fatalf("username update = %#v", username)
	}
}

func TestConfigurationResultEncodersAreExactAndSecretFree(t *testing.T) {
	value := ConfigurationResult{
		ConfigurationID: "campus", DisplayName: "校园网", InstitutionProfileID: "jlu",
		InstitutionDisplayName: "吉林大学", AuthenticationProtocolID: "drcom-5.2.0-d",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
	}
	data, err := MarshalConfigurationResult(value)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"configurationId":"campus","displayName":"校园网","institutionProfileId":"jlu","institutionDisplayName":"吉林大学","authenticationProtocolId":"drcom-5.2.0-d","username":"user","credentialStored":true,"storageProtection":"protected","autoLogin":false,"autoReconnect":false}`
	if string(data) != expected {
		t.Fatalf("configuration result = %s", data)
	}
	list, err := MarshalConfigurationListResult(ConfigurationListResult{StorageProtection: "protected"})
	if err != nil || string(list) != `{"storageProtection":"protected","configurations":[]}` {
		t.Fatalf("empty list = %s, %v", list, err)
	}
	removed, err := MarshalConfigurationRemoveResult(ConfigurationRemoveResult{ConfigurationID: "campus", Status: "removed"})
	if err != nil || string(removed) != `{"configurationId":"campus","status":"removed"}` {
		t.Fatalf("remove = %s, %v", removed, err)
	}
	for _, marker := range []string{"password", "private-password-marker", "diagnosticCause"} {
		if bytes.Contains(data, []byte(marker)) || bytes.Contains(list, []byte(marker)) || bytes.Contains(removed, []byte(marker)) {
			t.Fatalf("encoded result contains %q", marker)
		}
	}
}

func TestConfigurationAutoLoginConflictErrorCodeIsStable(t *testing.T) {
	if ErrorCodeConfigurationAutoLoginConflict != "configuration_auto_login_conflict" {
		t.Fatalf("ErrorCodeConfigurationAutoLoginConflict = %q", ErrorCodeConfigurationAutoLoginConflict)
	}
}

func TestConfigurationUpdatePayloadAcceptsAutoLoginAutoReconnect(t *testing.T) {
	autoLogin, err := DecodeConfigurationUpdatePayload([]byte(`{"configurationId":"campus","autoLogin":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if autoLogin.AutoLogin == nil || !*autoLogin.AutoLogin || autoLogin.AutoReconnect != nil {
		t.Fatalf("autoLogin update = %#v", autoLogin)
	}
	autoReconnect, err := DecodeConfigurationUpdatePayload([]byte(`{"configurationId":"campus","autoReconnect":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if autoReconnect.AutoReconnect == nil || *autoReconnect.AutoReconnect || autoReconnect.AutoLogin != nil {
		t.Fatalf("autoReconnect update = %#v", autoReconnect)
	}
}
