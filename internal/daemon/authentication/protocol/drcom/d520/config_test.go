package d520

import (
	"encoding/json"
	"testing"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

func fullConfigMap() map[string]any {
	return map[string]any{
		"serverAddress":                "127.0.0.1",
		"serverPort":                   61440,
		"localPort":                    map[string]any{"mode": "system_assigned"},
		"authVersionHex":               "2c00",
		"keepAliveVersionHex":          "dc02",
		"controlCheckStatusHex":        "20",
		"ipdogHex":                     "01",
		"adapterNumberHex":             "01",
		"osInfoHex":                    "940000000600000000000000280a000002000000",
		"challengePaddingHex":          "000000000000000000000000000000",
		"loginIPDogPaddingHex":         "00000000",
		"loginDHCPPaddingHex":          "0000000000000000",
		"loginAuthExtensionPaddingHex": "0000",
		"challengeTimeout":             "3s",
		"loginTimeout":                 "5s",
		"keepaliveTimeout":             "3s",
		"logoutTimeout":                "1s",
		"heartbeatInterval":            "20s",
		"busyMaxAttempts":              3,
		"busyBackoffMin":               "1s",
		"busyBackoffMax":               "2s",
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

// configWithLocalPortRaw returns a complete valid config whose localPort field
// is replaced by the supplied raw fragment. It uses string injection rather
// than re-marshaling a json.RawMessage so malformed and trailing localPort
// fragments reach the decoder instead of failing at marshal time. Every other
// required field stays valid, so a rejection is attributable to localPort.
func configWithLocalPortRaw(t *testing.T, localPortRaw string) protocol.InstitutionProtocolConfiguration {
	t.Helper()
	m := fullConfigMap()
	delete(m, "localPort")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	s := string(b)
	if len(s) == 0 || s[len(s)-1] != '}' {
		t.Fatalf("unexpected marshaled config shape: %s", s)
	}
	s = s[:len(s)-1] + `,"localPort":` + localPortRaw + "}"
	return protocol.InstitutionProtocolConfiguration(s)
}

// configWithFixedLocalPort returns a complete valid config with a fixed
// localPort value, overriding the collision-free system_assigned default.
func configWithFixedLocalPort(t *testing.T, value int) protocol.InstitutionProtocolConfiguration {
	t.Helper()
	return configWithOverride(t, map[string]any{"localPort": map[string]any{"mode": "fixed", "value": value}})
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
	if cfg.localPort.mode != localPortSystemAssigned {
		t.Fatalf("localPort mode = %d, want system_assigned", cfg.localPort.mode)
	}
	if cfg.localPort.value != 0 {
		t.Fatalf("localPort value = %d, want 0 for system_assigned", cfg.localPort.value)
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
	if cfg.loginIPDogPadding != ([4]byte{}) {
		t.Fatalf("loginIPDogPadding = %x, want all zero", cfg.loginIPDogPadding)
	}
	if cfg.loginDHCPPadding != ([8]byte{}) {
		t.Fatalf("loginDHCPPadding = %x, want all zero", cfg.loginDHCPPadding)
	}
	if cfg.loginAuthExtensionPadding != ([2]byte{}) {
		t.Fatalf("loginAuthExtensionPadding = %x, want all zero", cfg.loginAuthExtensionPadding)
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
		{"missing localPort", configWithout(t, "localPort")},
		{"missing authVersionHex", configWithout(t, "authVersionHex")},
		{"missing busyMaxAttempts", configWithout(t, "busyMaxAttempts")},
		{"missing challengePaddingHex", configWithout(t, "challengePaddingHex")},
		{"missing loginIPDogPaddingHex", configWithout(t, "loginIPDogPaddingHex")},
		{"missing loginDHCPPaddingHex", configWithout(t, "loginDHCPPaddingHex")},
		{"missing loginAuthExtensionPaddingHex", configWithout(t, "loginAuthExtensionPaddingHex")},
		{"unspecified endpoint", configWithOverride(t, map[string]any{"serverAddress": "0.0.0.0"})},
		{"multicast endpoint", configWithOverride(t, map[string]any{"serverAddress": "224.0.0.1"})},
		{"non-ipv4 endpoint", configWithOverride(t, map[string]any{"serverAddress": "::1"})},
		{"port zero", configWithOverride(t, map[string]any{"serverPort": 0})},
		{"port too large", configWithOverride(t, map[string]any{"serverPort": 65536})},
		{"authVersion wrong width", configWithOverride(t, map[string]any{"authVersionHex": "2c"})},
		{"controlCheckStatus wrong width", configWithOverride(t, map[string]any{"controlCheckStatusHex": "2000"})},
		{"osInfo wrong width", configWithOverride(t, map[string]any{"osInfoHex": "94"})},
		{"challengePadding wrong width", configWithOverride(t, map[string]any{"challengePaddingHex": "00"})},
		{"loginIPDogPadding wrong width", configWithOverride(t, map[string]any{"loginIPDogPaddingHex": "000000"})},
		{"loginDHCPPadding wrong width", configWithOverride(t, map[string]any{"loginDHCPPaddingHex": "00000000"})},
		{"loginAuthExtensionPadding wrong width", configWithOverride(t, map[string]any{"loginAuthExtensionPaddingHex": "000000"})},
		{"loginIPDogPadding non-hex", configWithOverride(t, map[string]any{"loginIPDogPaddingHex": "zz000000"})},
		{"loginDHCPPadding non-hex", configWithOverride(t, map[string]any{"loginDHCPPaddingHex": "zz00000000000000"})},
		{"loginAuthExtensionPadding non-hex", configWithOverride(t, map[string]any{"loginAuthExtensionPaddingHex": "zz00"})},
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

// TestConfigDecodesLocalPortBothForms proves both accepted localPort forms
// decode to the expected private immutable value.
func TestConfigDecodesLocalPortBothForms(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantMode localPortMode
		wantVal  uint16
	}{
		{"fixed", `{"mode":"fixed","value":61440}`, localPortFixed, 61440},
		{"fixed low port", `{"mode":"fixed","value":1}`, localPortFixed, 1},
		{"fixed high port", `{"mode":"fixed","value":65535}`, localPortFixed, 65535},
		{"system_assigned", `{"mode":"system_assigned"}`, localPortSystemAssigned, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := decodeInstitutionProtocolConfiguration(configWithLocalPortRaw(t, tc.raw))
			if err != nil {
				t.Fatalf("decode localPort %s: %v", tc.raw, err)
			}
			if cfg.localPort.mode != tc.wantMode {
				t.Fatalf("mode = %d, want %d", cfg.localPort.mode, tc.wantMode)
			}
			if cfg.localPort.value != tc.wantVal {
				t.Fatalf("value = %d, want %d", cfg.localPort.value, tc.wantVal)
			}
		})
	}
}

// TestConfigRejectsInvalidLocalPort proves the full strict localPort rejection
// matrix. Shape errors (a valid JSON value of the wrong type) reach
// decodeLocalPort and name the field; malformed or trailing localPort JSON is
// rejected at the top-level config parse. Errors never reproduce a raw
// caller-supplied mode value.
func TestConfigRejectsInvalidLocalPort(t *testing.T) {
	cases := []struct {
		name           string
		raw            string
		mustNameField  bool
		mustNotContain string
	}{
		{"null", "null", true, ""},
		{"non-object string", `"x"`, true, ""},
		{"non-object number", "5", true, ""},
		{"non-object array", "[]", true, ""},
		{"unknown nested field", `{"mode":"fixed","value":61440,"extra":1}`, true, ""},
		{"missing mode", `{"value":61440}`, true, ""},
		{"mode non-string", `{"mode":5}`, true, ""},
		{"unknown mode", `{"mode":"SECRETLOCALPORTMODE"}`, true, "SECRETLOCALPORTMODE"},
		{"fixed missing value", `{"mode":"fixed"}`, true, ""},
		{"fixed value zero", `{"mode":"fixed","value":0}`, true, ""},
		{"fixed value too large", `{"mode":"fixed","value":65536}`, true, ""},
		{"fixed value non-integer", `{"mode":"fixed","value":1.5}`, true, ""},
		{"fixed value non-number", `{"mode":"fixed","value":"x"}`, true, ""},
		{"system_assigned with value", `{"mode":"system_assigned","value":61440}`, true, ""},
		{"malformed", "{,", false, ""},
		{"trailing nested", `{"mode":"system_assigned"}{}`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeInstitutionProtocolConfiguration(configWithLocalPortRaw(t, tc.raw))
			if err == nil {
				t.Fatalf("localPort %q expected error, got nil", tc.raw)
			}
			if tc.mustNameField && !containsString(err.Error(), "localPort") {
				t.Fatalf("error does not name localPort: %v", err)
			}
			if tc.mustNotContain != "" && containsString(err.Error(), tc.mustNotContain) {
				t.Fatalf("error leaked raw value %q: %v", tc.mustNotContain, err)
			}
		})
	}
}

// TestConfigServerAndLocalPortsDecodedIndependently proves serverPort and
// localPort are independent endpoint dimensions, including the JLU shape where
// both equal 61440.
func TestConfigServerAndLocalPortsDecodedIndependently(t *testing.T) {
	jlu, err := decodeInstitutionProtocolConfiguration(configWithFixedLocalPort(t, 61440))
	if err != nil {
		t.Fatalf("decode JLU-shaped config: %v", err)
	}
	if jlu.serverPort != 61440 || jlu.localPort.mode != localPortFixed || jlu.localPort.value != 61440 {
		t.Fatalf("JLU ports = server %d local mode %d value %d, want 61440/fixed/61440",
			jlu.serverPort, jlu.localPort.mode, jlu.localPort.value)
	}

	differ, err := decodeInstitutionProtocolConfiguration(configWithOverride(t, map[string]any{
		"serverPort": 2048,
		"localPort":  map[string]any{"mode": "fixed", "value": 4096},
	}))
	if err != nil {
		t.Fatalf("decode differing-ports config: %v", err)
	}
	if differ.serverPort != 2048 || differ.localPort.value != 4096 {
		t.Fatalf("independent ports = server %d local %d, want 2048/4096",
			differ.serverPort, differ.localPort.value)
	}
}

// TestConfigLoginPaddingFieldsCaseInsensitive proves the three Login padding
// fields decode case-insensitively into their fixed-width values.
func TestConfigLoginPaddingFieldsCaseInsensitive(t *testing.T) {
	cfg, err := decodeInstitutionProtocolConfiguration(configWithOverride(t, map[string]any{
		"loginIPDogPaddingHex":         "AABBCCDD",
		"loginDHCPPaddingHex":          "AABBCCDDEEFF0011",
		"loginAuthExtensionPaddingHex": "AABB",
	}))
	if err != nil {
		t.Fatalf("decode uppercase padding: %v", err)
	}
	if cfg.loginIPDogPadding != [4]byte{0xaa, 0xbb, 0xcc, 0xdd} {
		t.Fatalf("loginIPDogPadding = %x, want aabbccdd", cfg.loginIPDogPadding)
	}
	if cfg.loginDHCPPadding != [8]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11} {
		t.Fatalf("loginDHCPPadding = %x, want aabbccddeeff0011", cfg.loginDHCPPadding)
	}
	if cfg.loginAuthExtensionPadding != [2]byte{0xaa, 0xbb} {
		t.Fatalf("loginAuthExtensionPadding = %x, want aabb", cfg.loginAuthExtensionPadding)
	}
}
