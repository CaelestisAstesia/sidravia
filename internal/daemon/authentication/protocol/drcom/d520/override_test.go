package d520

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

func overrideJSON(t *testing.T, fields map[string]any) protocol.AuthenticationProtocolContextOverride {
	t.Helper()
	obj := map[string]any{"schemaVersion": 1}
	for name, value := range fields {
		obj[name] = value
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOverrideStrictEnvelope(t *testing.T) {
	cases := []string{
		" ", "null", "[]", "true", `"object"`, "1", "{", "{}{}", "{} null", "{} garbage",
		`{"schemaVersion":1} true`, `{"schemaVersion":1,}`, `{"schemaVersion":1,"schemaVersion":1}`,
		`{"schemaVersion":1,"hostName":"a","host\u004eame":"b"}`,
		`{"hostName":"a"}`, `{"schemaVersion":null}`, `{"schemaVersion":"1"}`,
		`{"schemaVersion":1.0}`, `{"schemaVersion":1e0}`, `{"schemaVersion":2}`,
		`{"schemaVersion":1,"SECRETKEY":"SECRETVAL"}`,
		`{"schemaVersion":1,"hostName":SECRETVAL}`,
		"{\"schemaVersion\":1,\"hostName\":\"\xff\"}",
		strings.Repeat(" ", 16*1024) + "{}",
	}
	for i, raw := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			inputs := factoryInputs(validConfig(t), testCredential(), testBinding(t))
			inputs.ProtocolContextOverride = []byte(raw)
			err := NewFactory().ValidateProtocolContextOverride(inputs.ProtocolContextOverride)
			if err == nil {
				t.Fatal("invalid override accepted by validation")
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unsafe error: %v", err)
			}
			run, createErr := NewFactory().CreateAuthenticationProtocolRun(inputs)
			if createErr == nil || run != nil {
				t.Fatal("invalid override created a Run")
			}
			if strings.Contains(createErr.Error(), "SECRET") {
				t.Fatalf("unsafe creation error: %v", createErr)
			}
		})
	}
	raw := "{}" + strings.Repeat(" ", 16*1024-2)
	if err := NewFactory().ValidateProtocolContextOverride([]byte(raw)); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
}

func TestOverrideFieldBounds(t *testing.T) {
	cases := []struct {
		field   string
		invalid []any
	}{
		{"reportedIPv4", []any{nil, 1, true, []string{}, "0.0.0.0", "224.0.0.1", "255.255.255.255", "::ffff:127.0.0.1", "127.00.0.1", "127.0.0.1 ", "SECRETVAL"}},
		{"reportedMAC", []any{nil, 1, "00:00:00:00:00:00", "02-00-00-00-00-01", "AA:00:00:00:00:01", "0200.0000.0001", "02:00:00:00:00", "02:00:00:00:00:00:00:01"}},
		{"reportedDNSIPv4", []any{nil, "8.8.8.8", []any{nil}, []any{1}, []string{"224.0.0.1"}, []string{"255.255.255.255"}, []string{"::1"}, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}}},
		{"reportedDHCPIPv4", []any{nil, 1, "224.0.0.1", "255.255.255.255", "::1", " 0.0.0.0"}},
		{"hostName", []any{nil, 1, true, "a\x00b", "😀", strings.Repeat("a", 33), strings.Repeat("中", 17), "SECRETVAL😀"}},
		{"osFamily", []any{nil, 1, " Windows", "Windows ", "a\x00b", "😀", strings.Repeat("a", 33)}},
		{"osRelease", []any{nil, 1, " 10", "10 ", "a\x00b", "😀", strings.Repeat("中", 17)}},
	}
	hexFields := map[string]int{"authVersionHex": 2, "keepAliveVersionHex": 2, "controlCheckStatusHex": 1, "ipdogHex": 1, "adapterNumberHex": 1, "osInfoHex": 20, "challengePaddingHex": 15, "loginIPDogPaddingHex": 4, "loginDHCPPaddingHex": 8, "loginAuthExtensionPaddingHex": 2}
	for name, width := range hexFields {
		cases = append(cases, struct {
			field   string
			invalid []any
		}{name, []any{nil, 1, "", strings.Repeat("00", width-1), strings.Repeat("00", width+1), strings.Repeat("AA", width), strings.Repeat("gg", width)}})
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			for _, value := range tc.invalid {
				err := NewFactory().ValidateProtocolContextOverride(overrideJSON(t, map[string]any{tc.field: value}))
				if err == nil {
					t.Fatalf("invalid %s accepted (%T)", tc.field, value)
				}
				if strings.Contains(err.Error(), "SECRET") {
					t.Fatalf("unsafe error: %v", err)
				}
			}
		})
	}
	for _, field := range []string{"serverAddress", "serverPort", "localPort", "heartbeatInterval", "password", "username", "institutionID", "machineArchitecture", "salt", "authInfo", "tail", "serial", "rawPacket", "sessionID", "revision"} {
		if err := NewFactory().ValidateProtocolContextOverride(overrideJSON(t, map[string]any{field: "SECRETVAL"})); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("unsupported %s error = %v", field, err)
		}
	}
	if err := NewFactory().ValidateProtocolContextOverride([]byte(`{"schemaVersion":1,"hostName":"\ud800"}`)); err == nil {
		t.Fatal("unpaired surrogate accepted")
	}
	if err := NewFactory().ValidateProtocolContextOverride(overrideJSON(t, map[string]any{"osFamily": strings.Repeat("a", 16), "osRelease": strings.Repeat("b", 16)})); err == nil {
		t.Fatal("oversized combined OS accepted")
	}
}

func TestOverrideAcceptedBoundariesAndClears(t *testing.T) {
	cases := []map[string]any{
		{}, {"reportedIPv4": "127.0.0.1", "reportedMAC": "02:00:00:00:00:01"},
		{"reportedDNSIPv4": []string{}, "reportedDHCPIPv4": "0.0.0.0"},
		{"reportedDNSIPv4": []string{"0.0.0.0", "1.1.1.1"}},
		{"hostName": strings.Repeat("中", 16), "osFamily": "", "osRelease": strings.Repeat("a", 32)},
		{"osFamily": strings.Repeat("a", 15), "osRelease": strings.Repeat("b", 16)},
	}
	for _, fields := range cases {
		if err := NewFactory().ValidateProtocolContextOverride(overrideJSON(t, fields)); err != nil {
			t.Fatalf("valid boundary rejected: %v", err)
		}
	}
	for _, dns := range [][]string{{}, {"9.9.9.9"}, {"9.9.9.9", "0.0.0.0"}} {
		inputs := factoryInputs(validConfig(t), testCredential(), testBinding(t))
		inputs.ProtocolContextOverride = overrideJSON(t, map[string]any{"reportedDNSIPv4": dns, "reportedDHCPIPv4": "0.0.0.0"})
		run, err := NewFactory().CreateAuthenticationProtocolRun(inputs)
		if err != nil {
			t.Fatal(err)
		}
		login := run.(*d520Run).definition.login
		if login.secondaryDNS != ([4]byte{}) || login.dhcpIPv4 != ([4]byte{}) {
			t.Fatal("clear did not replace base values")
		}
		if len(dns) == 0 && login.primaryDNS != ([4]byte{}) {
			t.Fatal("empty DNS did not clear primary")
		}
	}
}

func TestOverrideReachesExistingWireFieldsWithoutChangingInputs(t *testing.T) {
	inputs := factoryInputs(validConfig(t), testCredential(), testBinding(t))
	fields := map[string]any{
		"reportedIPv4": "192.0.2.42", "reportedMAC": "02:03:04:05:06:07", "reportedDNSIPv4": []string{"9.8.7.6", "5.4.3.2"}, "reportedDHCPIPv4": "10.20.30.40",
		"hostName": "终端", "osFamily": "TestOS", "osRelease": "11",
		"authVersionHex": "ab12", "keepAliveVersionHex": "cd34", "controlCheckStatusHex": "35", "ipdogHex": "46", "adapterNumberHex": "57",
		"osInfoHex": strings.Repeat("ab", 20), "challengePaddingHex": strings.Repeat("bc", 15), "loginIPDogPaddingHex": "cdef1234", "loginDHCPPaddingHex": "1234567890abcdef", "loginAuthExtensionPaddingHex": "ef56",
	}
	inputs.ProtocolContextOverride = overrideJSON(t, fields)
	originalConfig := bytes.Clone(inputs.InstitutionProtocolConfiguration)
	originalOverride := bytes.Clone(inputs.ProtocolContextOverride)
	originalHost := inputs.SystemHostInformation
	run, err := NewFactory().CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inputs.InstitutionProtocolConfiguration, originalConfig) || !bytes.Equal(inputs.ProtocolContextOverride, originalOverride) || inputs.SystemHostInformation != originalHost || inputs.SelectedSystemNetworkBinding.LocalIPv4AddressAssignment().Address.String() != "127.0.0.1" {
		t.Fatal("Factory changed source inputs")
	}
	def := run.(*d520Run).definition
	salt := [4]byte{1, 2, 3, 4}
	login, err := buildLoginRequest(def.login, salt)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		start, end int
		want       []byte
	}{
		{81, 85, []byte{192, 0, 2, 42}}, {97, 105, md5C([4]byte{192, 0, 2, 42})},
		{142, 146, []byte{9, 8, 7, 6}}, {146, 150, []byte{10, 20, 30, 40}}, {150, 154, []byte{5, 4, 3, 2}},
		{106, 110, mustHex(t, "cdef1234")}, {154, 162, mustHex(t, "1234567890abcdef")}, {162, 182, bytes.Repeat([]byte{0xab}, 20)},
		{310, 312, []byte{0xab, 0x12}}, {320, 326, []byte{2, 3, 4, 5, 6, 7}}, {326, 328, []byte{0xef, 0x56}},
	}
	for _, check := range checks {
		if !bytes.Equal(login[check.start:check.end], check.want) {
			t.Fatalf("wire region [%d,%d) mismatch", check.start, check.end)
		}
	}
	if login[56] != 0x35 || login[57] != 0x57 || login[105] != 0x46 {
		t.Fatal("fixed bytes did not reach wire")
	}
	hostName, _ := encodeProtocolText("终端")
	if !bytes.Equal(login[110:110+len(hostName)], hostName) || !bytes.Equal(login[182:191], []byte("TestOS 11")) {
		t.Fatal("host override did not reach wire")
	}
	digestA := md5A(salt[:], []byte(testCredential().Password))
	for i := 0; i < 6; i++ {
		if login[58+i]^digestA[i] != byte(i+2) {
			t.Fatal("MAC XOR does not match final MAC")
		}
	}
	if !bytes.Equal(login[314:318], crc1968(loginCRCTailInput(login))) {
		t.Fatal("CRC does not cover effective fields")
	}
	challenge := buildChallengeRequest(1, def.cfg.challengePadding)
	if !bytes.Equal(challenge[5:20], bytes.Repeat([]byte{0xbc}, 15)) {
		t.Fatal("challenge override missing")
	}
	ka2, err := buildKA2Request(2, ka2Type3, false, def.cfg.keepAliveVersion, [4]byte{}, def.login.clientIPv4)
	if err != nil || !bytes.Equal(ka2[6:8], []byte{0xcd, 0x34}) || !bytes.Equal(ka2[28:32], []byte{192, 0, 2, 42}) {
		t.Fatal("KA2 effective fields missing")
	}
	logout, err := buildLogoutRequest(def.login, salt, [16]byte{})
	if err != nil || logout[56] != 0x35 || logout[57] != 0x57 || !bytes.Equal(logout[58:64], login[58:64]) {
		t.Fatal("Logout effective fields missing")
	}
	inputs.InstitutionProtocolConfiguration[0] = 0
	inputs.ProtocolContextOverride[0] = 0
	inputs.SystemHostInformation.HostName = "mutated"
	inputs.SelectedSystemNetworkBinding = buildBinding(t, "127.0.0.2", []byte{1, 2, 3, 4, 5, 6}, nil, nil)
	if run.(*d520Run).definition != def || def.socketSourceIPv4 != ([4]byte{127, 0, 0, 1}) {
		t.Fatal("Run retained mutable input")
	}
	fresh, err := NewFactory().CreateAuthenticationProtocolRun(factoryInputs(validConfig(t), testCredential(), testBinding(t)))
	if err != nil || fresh.(*d520Run).definition.login.clientIPv4 != ([4]byte{127, 0, 0, 1}) {
		t.Fatal("Factory retained override state")
	}
}
