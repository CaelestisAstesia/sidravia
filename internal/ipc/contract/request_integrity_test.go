package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRequestIntegrityCanonicalNames(t *testing.T) {
	decoders := []struct {
		name, data, field string
		decode            func([]byte) error
	}{
		{"configuration get", `{"configurationId":"a"}`, "configurationId", func(b []byte) error { _, e := DecodeConfigurationIDPayload(b); return e }},
		{"configuration create", `{"configurationId":"a","institutionProfileId":"p","username":"u","password":"s","allowInsecureStorage":false,"autoLogin":false,"autoReconnect":true,"networkBindingPolicy":{"mode":"automatically_select_latest_available"}}`, "username", func(b []byte) error { _, e := DecodeConfigurationCreatePayload(b); return e }},
		{"configuration update", `{"configurationId":"a","displayName":"new"}`, "configurationId", func(b []byte) error { _, e := DecodeConfigurationUpdatePayload(b); return e }},
		{"configuration password", `{"configurationId":"a","password":"s","allowInsecureStorage":false}`, "password", func(b []byte) error { _, e := DecodeConfigurationSetPasswordPayload(b); return e }},
		{"configuration remove", `{"configurationId":"a","allowInsecureStorage":false}`, "allowInsecureStorage", func(b []byte) error { _, e := DecodeConfigurationRemovePayload(b); return e }},
		{"session start", string(validStartJSON()), "username", func(b []byte) error { _, e := DecodeSessionStartOneShotPayload(b); return e }},
		{"session stop", `{"sessionId":"a"}`, "sessionId", func(b []byte) error { _, e := DecodeSessionStopPayload(b); return e }},
		{"session get", `{"sessionId":"a"}`, "sessionId", func(b []byte) error { _, e := DecodeSessionGetPayload(b); return e }},
		{"session ensure", `{"sessionId":"a"}`, "sessionId", func(b []byte) error { _, e := DecodeSessionEnsureRunningPayload(b); return e }},
		{"session restart", `{"sessionId":"a"}`, "sessionId", func(b []byte) error { _, e := DecodeSessionRestartPayload(b); return e }},
		{"session remove", `{"sessionId":"a"}`, "sessionId", func(b []byte) error { _, e := DecodeSessionRemovePayload(b); return e }},
	}
	for _, tc := range decoders {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.decode([]byte(tc.data)); e != nil {
				t.Fatalf("canonical input: %v", e)
			}
			alias := strings.ToUpper(tc.field[:1]) + tc.field[1:]
			escapedAlias := fmt.Sprintf(`\u%04x%s`, alias[0], alias[1:])
			for _, field := range []string{alias, strings.ToUpper(tc.field), escapedAlias} {
				data := strings.Replace(tc.data, `"`+tc.field+`":`, `"`+field+`":`, 1)
				if e := tc.decode([]byte(data)); e == nil {
					t.Errorf("accepted alias %q", field)
				}
			}
		})
	}
	for _, data := range []string{
		`{"configurationId":"a","ConfigurationId":"b","displayName":"new"}`,
		`{"configurationId":"a","displayName":"new","networkBindingPolicy":{"Mode":"automatically_select_latest_available"}}`,
	} {
		if _, e := DecodeConfigurationUpdatePayload([]byte(data)); e == nil {
			t.Error("accepted ambiguous update")
		}
	}
}

func TestRequestIntegrityUnicode(t *testing.T) {
	for _, bad := range []string{`x\ud800y`, `x\udc00y`, `\ud800\u0041`, "x" + string([]byte{0xff}) + "y"} {
		data := `{"configurationId":"a","password":"PRIVATE-SECRET-` + bad + `","allowInsecureStorage":false}`
		if _, e := DecodeConfigurationSetPasswordPayload([]byte(data)); e == nil {
			t.Error("accepted corrupt password")
		} else if strings.Contains(e.Error(), "PRIVATE-SECRET") {
			t.Fatal("secret leaked")
		}
		update := `{"configurationId":"a","password":"` + bad + `"}`
		if _, e := DecodeConfigurationUpdatePayload([]byte(update)); e == nil {
			t.Error("accepted corrupt update password")
		}
		create := strings.Replace(`{"configurationId":"a","institutionProfileId":"p","username":"u","password":"s","allowInsecureStorage":false,"autoLogin":false,"autoReconnect":true,"networkBindingPolicy":{"mode":"automatically_select_latest_available"}}`, `"s"`, `"`+bad+`"`, 1)
		if _, e := DecodeConfigurationCreatePayload([]byte(create)); e == nil {
			t.Error("accepted corrupt create password")
		}
		start := strings.Replace(string(validStartJSON()), `"secret"`, `"`+bad+`"`, 1)
		if _, e := DecodeSessionStartOneShotPayload([]byte(start)); e == nil {
			t.Error("accepted corrupt start password")
		}
	}
	for _, tc := range []struct{ wire, want string }{{"中文", "中文"}, {`\ud83d\ude00`, "😀"}, {"�", "�"}, {`\ufffd`, "�"}, {`x\\ud800y`, `x\ud800y`}, {"", ""}} {
		p, e := DecodeConfigurationSetPasswordPayload([]byte(`{"configurationId":"a","password":"` + tc.wire + `","allowInsecureStorage":false}`))
		if e != nil || p.Password != tc.want {
			t.Errorf("valid Unicode altered: %v", e)
		}
	}
	for _, data := range []string{`{"configurationId":"\ud800","displayName":"new"}`, `{"configurationId":"a","displayName":"\udc00"}`, `{"configurationId":"a","displayName":"new","networkBindingPolicy":{"mode":"explicit_interface_and_local_ipv4","interfaceId":"\ud800","localIpv4Address":"192.0.2.1"}}`} {
		if _, e := DecodeConfigurationUpdatePayload([]byte(data)); e == nil {
			t.Error("accepted corrupt identity/nested Unicode")
		}
	}
	p, e := DecodeConfigurationUpdatePayload([]byte(`{"configurationId":"a","protocolContextOverride":{"x":1,"X":2}}`))
	if e != nil || string(p.ProtocolContextOverride) != `{"x":1,"X":2}` {
		t.Fatalf("opaque override changed: %v", e)
	}
}

func TestRequestIntegrityEnvelope(t *testing.T) {
	for _, data := range []string{
		`{"Kind":"request","id":"1","method":"daemon.status","payload":{}}`,
		`{"kind":"request","id":"1","ID":"2","method":"daemon.status","payload":{}}`,
		`{"kind":"request","id":"1","method":"daemon.status","Method":"daemon.stop","payload":{}}`,
		`{"kind":"request","id":"1","method":"daemon.status","Payload":{}}`,
		`{"kind":"request","id":"\ud800","method":"daemon.status","payload":{}}`,
		`{"kind":"request","id":"1","method":"daemon.status","payload":{"x":"\udc00"}}`,
		`{"kind":"request","id":"1","method":"daemon.status","payload":{"\ud800":1}}`,
	} {
		if _, e := DecodeRequest([]byte(data)); e == nil {
			t.Error("accepted corrupt/ambiguous envelope")
		}
	}
	data := []byte(`{"kind":"request","id":"1","method":"daemon.status","payload":{"x":"` + string([]byte{0xff}) + `"}}`)
	if _, e := DecodeRequest(data); e == nil {
		t.Error("accepted raw invalid UTF8")
	}
	raw, e := json.Marshal(Request{Kind: "request", ID: "中文�😀", Method: "daemon.status", Payload: json.RawMessage(`{}`)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeRequest(raw); e != nil {
		t.Fatal(e)
	}
}
