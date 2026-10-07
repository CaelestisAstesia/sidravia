package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

const testNetworkResult = `{"available":true,"revision":1,"interfaces":[{"interfaceId":"loopback","displayName":"","operationalState":"up","physicalMedium":"unknown","hardwareBacked":false,"physicalConnectorPresent":false,"filterInterface":false,"endpointInterface":false,"addressAssignmentMethod":"unknown","ipv4Assignments":[{"address":"127.0.0.1","prefixLength":8,"automaticCandidate":false,"explicitBindable":true}]}],"observedAt":"2026-10-07T01:02:03.123456789Z"}`

func TestNetworkResultStrictShapeAndValues(t *testing.T) {
	for _, good := range []string{testNetworkResult, `{"available":false,"revision":0,"interfaces":[]}`, strings.Replace(testNetworkResult, `"revision":1`, `"revision":18446744073709551615`, 1), strings.Replace(testNetworkResult, `"prefixLength":8`, `"prefixLength":32`, 1)} {
		result, err := DecodeNetworkInterfacesResult([]byte(good))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := MarshalNetworkInterfacesResult(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeNetworkInterfacesResult(wire); err != nil {
			t.Fatal(err)
		}
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(testNetworkResult), &root); err != nil {
		t.Fatal(err)
	}
	row := root["interfaces"].([]any)[0].(map[string]any)
	address := row["ipv4Assignments"].([]any)[0].(map[string]any)
	for _, object := range []map[string]any{root, row, address} {
		for key, value := range object {
			delete(object, key)
			data, _ := json.Marshal(root)
			if _, err := DecodeNetworkInterfacesResult(data); err == nil {
				t.Fatalf("missing %s accepted", key)
			}
			object[key] = nil
			data, _ = json.Marshal(root)
			if _, err := DecodeNetworkInterfacesResult(data); err == nil {
				t.Fatalf("null %s accepted", key)
			}
			object[key] = map[string]any{}
			data, _ = json.Marshal(root)
			if _, err := DecodeNetworkInterfacesResult(data); err == nil {
				t.Fatalf("wrong type %s accepted", key)
			}
			object[key] = value
		}
		object["password"] = "private"
		data, _ := json.Marshal(root)
		if _, err := DecodeNetworkInterfacesResult(data); err == nil {
			t.Fatal("unknown secret field accepted")
		}
		delete(object, "password")
	}
	for _, bad := range []string{
		`null`, `[]`, `{}`, testNetworkResult + ` {}`,
		strings.Replace(testNetworkResult, `"available":true`, `"available":false`, 1),
		strings.Replace(testNetworkResult, `"revision":1`, `"revision":0`, 1),
		strings.Replace(testNetworkResult, `"revision":1`, `"revision":-1`, 1),
		strings.Replace(testNetworkResult, `"revision":1`, `"revision":1.0`, 1),
		strings.Replace(testNetworkResult, `"revision":1`, `"revision":1e0`, 1),
		strings.Replace(testNetworkResult, `"revision":1`, `"revision":18446744073709551616`, 1),
		strings.Replace(testNetworkResult, `"prefixLength":8`, `"prefixLength":33`, 1),
		strings.Replace(testNetworkResult, `"address":"127.0.0.1"`, `"address":"127.00.0.1"`, 1),
		strings.Replace(testNetworkResult, `"address":"127.0.0.1"`, `"address":"::1"`, 1),
		strings.Replace(testNetworkResult, `"operationalState":"up"`, `"operationalState":"unknown"`, 1),
		strings.Replace(testNetworkResult, `"physicalMedium":"unknown"`, `"physicalMedium":"loopback"`, 1),
		strings.Replace(testNetworkResult, `"addressAssignmentMethod":"unknown"`, `"addressAssignmentMethod":"other"`, 1),
		strings.Replace(testNetworkResult, `"available":true`, `"available":true,"Available":true`, 1),
		strings.Replace(testNetworkResult, `"revision":1`, `"revision":1,"\u0072evision":1`, 1),
		strings.Replace(testNetworkResult, `"address":"127.0.0.1"`, `"address":"127.0.0.1","address":"127.0.0.1"`, 1),
		strings.Replace(testNetworkResult, `2026-10-07T01:02:03.123456789Z`, `2026-02-30T01:02:03Z`, 1),
		strings.Replace(testNetworkResult, `2026-10-07T01:02:03.123456789Z`, `0001-01-01T00:00:00Z`, 1),
		strings.Replace(testNetworkResult, `2026-10-07T01:02:03.123456789Z`, `2026-10-07T01:02:03+24:00`, 1),
		strings.Replace(testNetworkResult, `2026-10-07T01:02:03.123456789Z`, `2026-10-07T01:02:03.1234567891Z`, 1),
	} {
		if _, err := DecodeNetworkInterfacesResult([]byte(bad)); err == nil {
			t.Fatalf("accepted bad result %s", bad)
		}
	}
}

func TestNetworkResultRejectsMalformedOriginalUnicodeSafely(t *testing.T) {
	for _, field := range []string{"interfaceId", "displayName"} {
		old := `"displayName":""`
		if field == "interfaceId" {
			old = `"interfaceId":"loopback"`
		}
		for _, malformed := range []string{
			`\ud800`, `\udfff`, `\ud800\u0041`, `\udc00\ud800`,
			`\ud800 \udc00`, `\ud800\\udc00`,
			string([]byte{0xff}), string([]byte{0xed, 0xa0, 0x80}),
			string([]byte{0xc0, 0xaf}), string([]byte{0xe2, 0x9f}),
		} {
			wire := strings.Replace(testNetworkResult, old, `"`+field+`":"private-marker`+malformed+`"`, 1)
			_, err := DecodeNetworkInterfacesResult([]byte(wire))
			if err == nil || err.Error() != "decode payload: invalid network result Unicode" {
				t.Fatalf("%s malformed Unicode did not return fixed safe error: %v", field, err)
			}
		}
	}
	for _, wire := range []string{
		strings.Replace(testNetworkResult, `"displayName"`, `"private-key\ud800"`, 1),
		strings.Replace(testNetworkResult, `"address"`, `"private-key\udfff"`, 1),
		strings.Replace(testNetworkResult, `"address"`, `"private-key`+string([]byte{0xff})+`"`, 1),
	} {
		_, err := DecodeNetworkInterfacesResult([]byte(wire))
		if err == nil || err.Error() != "decode payload: invalid network result Unicode" {
			t.Fatalf("nested malformed key did not return fixed safe error: %v", err)
		}
	}
}

func TestNetworkResultRetainsValidUnicodeSpellings(t *testing.T) {
	for _, test := range []struct{ wire, want string }{
		{`"\ud83d\ude00"`, "😀"},
		{`"\uD83D\uDe00"`, "😀"},
		{`"网卡"`, "网卡"},
		{`"𐐷"`, "𐐷"},
		{`"�"`, "�"},
		{`"\ufffd"`, "�"},
		{`"literal\\ud800"`, `literal\ud800`},
		{`"quote\"then\\ud800"`, `quote"then\ud800`},
		{`"path\\\\ud800"`, `path\\ud800`},
	} {
		for _, field := range []string{"interfaceId", "displayName"} {
			old := `"displayName":""`
			if field == "interfaceId" {
				old = `"interfaceId":"loopback"`
			}
			wire := strings.Replace(testNetworkResult, old, `"`+field+`":`+test.wire, 1)
			result, err := DecodeNetworkInterfacesResult([]byte(wire))
			if err != nil {
				t.Fatalf("valid %s rejected: %v", field, err)
			}
			got := result.Interfaces[0].DisplayName
			if field == "interfaceId" {
				got = result.Interfaces[0].InterfaceID
			}
			if got != test.want {
				t.Fatalf("%s Unicode value = %q, want %q", field, got, test.want)
			}
		}
	}
	wire := strings.Replace(testNetworkResult, `"displayName"`, `"\u0064isplayName"`, 1)
	result, err := DecodeNetworkInterfacesResult([]byte(wire))
	if err != nil || result.Interfaces[0].DisplayName != "" {
		t.Fatal("legal escaped key or empty name rejected")
	}
}
