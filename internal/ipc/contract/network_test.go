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
