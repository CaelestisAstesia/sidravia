package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// Socket facts remain part of session state after removing independent probes.
func TestPassiveSocketContractRetainsStrictValidation(t *testing.T) {
	value := validPublicSessionResult()
	now := "2026-10-08T01:02:03Z"
	value.ProtocolSocket = NetworkProtocolSocket{State: "closed", RunGeneration: 1, UpdatedAt: &now,
		LocalEndpoint: &NetworkEndpoint{Address: "127.0.0.1", Port: 1234}, RemoteEndpoint: &NetworkEndpoint{Address: "198.51.100.1", Port: 61440}}
	raw, err := MarshalSessionResult(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{`"runGeneration":1`, `"runGeneration":0`}, {`"port":1234`, `"port":0`}, {`"127.0.0.1"`, `"255.255.255.255"`}, {`"198.51.100.1"`, `"198.051.100.1"`}, {`"updatedAt":"` + now + `"`, `"updatedAt":"bad"`}, {`"state":"closed"`, `"state":"future"`}} {
		if _, err := DecodeSessionResult([]byte(strings.Replace(string(raw), pair[0], pair[1], 1))); err == nil {
			t.Fatalf("invalid passive socket accepted: %s", pair[1])
		}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["protocolSocket"]; !ok {
		t.Fatal("passive socket projection lost")
	}
}
