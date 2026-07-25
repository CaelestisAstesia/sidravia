package d520

import (
	"encoding/json"
	"testing"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

func fullConfigMap() map[string]any {
	return map[string]any{
		"serverAddress":         "127.0.0.1",
		"serverPort":            61440,
		"authVersionHex":        "2c00",
		"keepAliveVersionHex":   "dc02",
		"controlCheckStatusHex": "20",
		"ipdogHex":              "01",
		"adapterNumberHex":      "01",
		"osInfoHex":             "940000000600000000000000280a000002000000",
		"challengePaddingHex":   "000000000000000000000000000000",
		"challengeTimeout":      "3s",
		"loginTimeout":          "5s",
		"keepaliveTimeout":      "3s",
		"logoutTimeout":         "1s",
		"heartbeatInterval":     "20s",
		"busyMaxAttempts":       3,
		"busyBackoffMin":        "1s",
		"busyBackoffMax":        "2s",
	}
}

func marshalConfig(t *testing.T, m map[string]any) protocol.InstitutionProtocolConfiguration {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return protocol.InstitutionProtocolConfiguration(b)
}

func configWithOverride(t *testing.T, overrides map[string]any) protocol.InstitutionProtocolConfiguration {
	t.Helper()
	m := fullConfigMap()
	for k, v := range overrides {
		m[k] = v
	}
	return marshalConfig(t, m)
}

func configWithout(t *testing.T, field string) protocol.InstitutionProtocolConfiguration {
	t.Helper()
	m := fullConfigMap()
	delete(m, field)
	return marshalConfig(t, m)
}

func validConfig(t *testing.T) protocol.InstitutionProtocolConfiguration {
	t.Helper()
	return marshalConfig(t, fullConfigMap())
}

func TestConfigDecodesValidCompleteProfile(t *testing.T) {
	cfg, err := decodeInstitutionProtocolConfiguration(validConfig(t))
	if err != nil {
		t.Fatalf("decode valid config: %v", err)
	}
	if cfg.serverAddress.String() != "127.0.0.1" {
		t.Fatalf("serverAddress = %s, want 127.0.0.1", cfg.serverAddress)
	}
	if cfg.serverPort != 61440 {
		t.Fatalf("serverPort = %d, want 61440", cfg.serverPort)
	}
	if cfg.authVersion != ([2]byte{0x2c, 0x00}) {
		t.Fatalf("authVersion = %x, want 2c00", cfg.authVersion)
	}
	if cfg.keepAliveVersion != ([2]byte{0xdc, 0x02}) {
		t.Fatalf("keepAliveVersion = %x, want dc02", cfg.keepAliveVersion)
	}
	if cfg.controlCheckStatus != 0x20 {
		t.Fatalf("controlCheckStatus = %x, want 20", cfg.controlCheckStatus)
	}
	if cfg.ipdog != 0x01 {
		t.Fatalf("ipdog = %x, want 01", cfg.ipdog)
	}
	if cfg.adapterNumber != 0x01 {
		t.Fatalf("adapterNumber = %x, want 01", cfg.adapterNumber)
	}
	if cfg.osInfo[0] != 0x94 || cfg.osInfo[19] != 0x00 {
		t.Fatalf("osInfo = %x, want 940000000600000000000000280a000002000000", cfg.osInfo)
	}
	if cfg.challengePadding != ([15]byte{}) {
		t.Fatalf("challengePadding = %x, want all zero", cfg.challengePadding)
	}
	if cfg.busyMaxAttempts != 3 {
		t.Fatalf("busyMaxAttempts = %d, want 3", cfg.busyMaxAttempts)
	}
	if cfg.busyBackoffMin != 1*time.Second || cfg.busyBackoffMax != 2*time.Second {
		t.Fatalf("backoff durations = min=%s max=%s, want 1s/2s", cfg.busyBackoffMin, cfg.busyBackoffMax)
	}
}

func TestConfigRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		raw  protocol.InstitutionProtocolConfiguration
	}{
		{"nil", protocol.InstitutionProtocolConfiguration(nil)},
		{"null", protocol.InstitutionProtocolConfiguration("null")},
		{"array", protocol.InstitutionProtocolConfiguration("[]")},
		{"scalar string", protocol.InstitutionProtocolConfiguration(`"x"`)},
		{"scalar number", protocol.InstitutionProtocolConfiguration("5")},
		{"malformed", protocol.InstitutionProtocolConfiguration("{")},
		{"trailing", protocol.InstitutionProtocolConfiguration(`{"serverAddress":"127.0.0.1"} extra`)},
		{"missing serverAddress", configWithout(t, "serverAddress")},
		{"missing serverPort", configWithout(t, "serverPort")},
		{"missing authVersionHex", configWithout(t, "authVersionHex")},
		{"missing busyMaxAttempts", configWithout(t, "busyMaxAttempts")},
		{"missing challengePaddingHex", configWithout(t, "challengePaddingHex")},
		{"unspecified endpoint", configWithOverride(t, map[string]any{"serverAddress": "0.0.0.0"})},
		{"multicast endpoint", configWithOverride(t, map[string]any{"serverAddress": "224.0.0.1"})},
		{"non-ipv4 endpoint", configWithOverride(t, map[string]any{"serverAddress": "::1"})},
		{"port zero", configWithOverride(t, map[string]any{"serverPort": 0})},
		{"port too large", configWithOverride(t, map[string]any{"serverPort": 65536})},
		{"authVersion wrong width", configWithOverride(t, map[string]any{"authVersionHex": "2c"})},
		{"controlCheckStatus wrong width", configWithOverride(t, map[string]any{"controlCheckStatusHex": "2000"})},
		{"osInfo wrong width", configWithOverride(t, map[string]any{"osInfoHex": "94"})},
		{"challengePadding wrong width", configWithOverride(t, map[string]any{"challengePaddingHex": "00"})},
		{"non-hex value", configWithOverride(t, map[string]any{"authVersionHex": "zz"})},
		{"invalid duration", configWithOverride(t, map[string]any{"challengeTimeout": "not-a-duration"})},
		{"non-positive duration", configWithOverride(t, map[string]any{"challengeTimeout": "0s"})},
		{"busyMaxAttempts less than one", configWithOverride(t, map[string]any{"busyMaxAttempts": 0})},
		{"backoff min greater than max", configWithOverride(t, map[string]any{"busyBackoffMin": "3s", "busyBackoffMax": "2s"})},
		{"non-integer port", protocol.InstitutionProtocolConfiguration(`{"serverAddress":"127.0.0.1","serverPort":"x"}`)},
		{"non-string serverAddress", protocol.InstitutionProtocolConfiguration(`{"serverAddress":123,"serverPort":61440}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeInstitutionProtocolConfiguration(tc.raw); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestConfigRejectsUnknownField(t *testing.T) {
	// Start from a complete valid Profile and add one unknown field, preserving
	// every required field. Decoding must fail because of the unknown field.
	raw := configWithOverride(t, map[string]any{"extraUnknownField": 1})
	_, err := decodeInstitutionProtocolConfiguration(raw)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !containsString(err.Error(), "extraUnknownField") {
		t.Fatalf("error does not name the unknown field: %v", err)
	}
	// The error must not reproduce the field value or the raw JSON.
	rawJSON := string(raw)
	if containsString(err.Error(), rawJSON) {
		t.Fatalf("error reproduced raw JSON: %v", err)
	}
}

func TestConfigAcceptsHexCaseInsensitively(t *testing.T) {
	cfg, err := decodeInstitutionProtocolConfiguration(configWithOverride(t, map[string]any{"authVersionHex": "2C00"}))
	if err != nil {
		t.Fatalf("decode uppercase hex: %v", err)
	}
	if cfg.authVersion != ([2]byte{0x2c, 0x00}) {
		t.Fatalf("authVersion = %x, want 2c00 (case-insensitive)", cfg.authVersion)
	}
}

func TestConfigErrorsDoNotLeakRawJSON(t *testing.T) {
	secret := "this-should-not-leak"
	raw := configWithOverride(t, map[string]any{"serverAddress": secret})
	_, err := decodeInstitutionProtocolConfiguration(raw)
	if err == nil {
		t.Fatal("expected error")
	}
	if containsString(err.Error(), secret) {
		t.Fatalf("error leaked raw JSON value %q: %v", secret, err)
	}
}

func TestOverrideAcceptsAbsentOrEmpty(t *testing.T) {
	cases := []protocol.AuthenticationProtocolContextOverride{
		nil,
		protocol.AuthenticationProtocolContextOverride(`{}`),
		protocol.AuthenticationProtocolContextOverride(`  {  }  `),
	}
	for _, raw := range cases {
		if err := validateProtocolContextOverride(raw); err != nil {
			t.Fatalf("override %q expected acceptance, got %v", raw, err)
		}
	}
}

func TestOverrideRejectsNonEmptyOrMalformed(t *testing.T) {
	cases := []string{
		"null",
		`{"a":1}`,
		`[]`,
		`"x"`,
		"5",
		"{",
		`{}{}`,
		`{"a":1} extra`,
	}
	for _, raw := range cases {
		if err := validateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride(raw)); err == nil {
			t.Fatalf("override %q expected error, got nil", raw)
		}
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
