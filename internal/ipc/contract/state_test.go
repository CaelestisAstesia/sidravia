package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func emptyStateBootstrap() StateBootstrap {
	return StateBootstrap{Sessions: SessionListResult{Sessions: []SessionResult{}, CleanupRequiredSessionIDs: []string{}}, Network: NetworkInterfacesResult{Interfaces: []NetworkInterfaceResult{}}}
}

func TestStateBootstrapStrictOwnedFactsAndLimits(t *testing.T) {
	value := emptyStateBootstrap()
	value.Sessions.Sessions = []SessionResult{validPublicSessionResult()}
	value.Sessions.CleanupRequiredSessionIDs = []string{"s"}
	network, err := DecodeNetworkInterfacesResult([]byte(testNetworkResult))
	if err != nil {
		t.Fatal(err)
	}
	value.Network = network
	raw, err := MarshalStateBootstrap(value)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeStateBootstrap(raw)
	if err != nil || !reflect.DeepEqual(got, value) {
		t.Fatal("complete bootstrap changed", err)
	}
	got.Sessions.CleanupRequiredSessionIDs[0] = "mutated"
	got.Network.Interfaces[0].IPv4Assignments[0].Address = "192.0.2.1"
	if value.Sessions.CleanupRequiredSessionIDs[0] != "s" || value.Network.Interfaces[0].IPv4Assignments[0].Address != "127.0.0.1" {
		t.Fatal("decoded facts alias input")
	}
	for _, bad := range []string{
		`null`, `[]`, `{}`, `{"sessions":null,"network":{}}`,
		strings.Replace(string(raw), `"sessions":{`, `"Sessions":{`, 1),
		strings.Replace(string(raw), `"network":`, `"extra":`, 1),
		strings.Replace(string(raw), `"network":`, `"network":{},"\u006eetwork":`, 1),
		strings.Replace(string(raw), `"accountName":"a"`, `"accountName":"\ud800"`, 1),
		strings.Replace(string(raw), `"updatedAt":"2026-10-07T01:02:03Z"`, `"updatedAt":"2026-02-30T00:00:00Z"`, 1),
		string(raw) + `{}`, string(raw) + `!`,
	} {
		if _, err := DecodeStateBootstrap([]byte(bad)); err == nil {
			t.Fatalf("invalid bootstrap accepted: %s", bad)
		}
	}
	emptyRaw, err := MarshalStateBootstrap(emptyStateBootstrap())
	if err != nil {
		t.Fatal(err)
	}
	exact := append(append([]byte{}, emptyRaw...), []byte(strings.Repeat(" ", StateFrameLimit-len(emptyRaw)))...)
	if _, err := DecodeStateBootstrap(exact); err != nil {
		t.Fatal("exact frame boundary rejected", err)
	}
	if _, err := DecodeStateBootstrap(append(exact, ' ')); err == nil {
		t.Fatal("oversize bootstrap accepted")
	}
	value.Sessions.Sessions[0].DisplayName = strings.Repeat("a", StateFrameLimit)
	if _, err := MarshalStateBootstrap(value); err == nil {
		t.Fatal("marshal exceeded frame limit")
	}
	// All 255 Session resources plus the unavailable network fit the resource
	// capacity. Their full required wire fields independently exceed 64 KiB.
	bounded := emptyStateBootstrap()
	for i := 0; i < StateResourceCapacity-1; i++ {
		s := validPublicSessionResult()
		s.AuthenticationSessionID = fmt.Sprint(i)
		bounded.Sessions.Sessions = append(bounded.Sessions.Sessions, s)
	}
	if _, err := MarshalStateBootstrap(bounded); err == nil || !strings.Contains(err.Error(), "frame limit") {
		t.Fatalf("exact resource boundary was not admitted to frame check: %v", err)
	}
	s := validPublicSessionResult()
	s.AuthenticationSessionID = "last"
	bounded.Sessions.Sessions = append(bounded.Sessions.Sessions, s)
	if _, err := MarshalStateBootstrap(bounded); err == nil || !strings.Contains(err.Error(), "resource capacity") {
		t.Fatalf("network resource was not counted: %v", err)
	}
	value = emptyStateBootstrap()
	value.Sessions.Sessions = []SessionResult{validPublicSessionResult()}
	value.Sessions.Sessions[0].AccountName = string([]byte{0xff})
	if _, err := MarshalStateBootstrap(value); err == nil {
		t.Fatal("malformed Go Unicode replaced")
	}
}

func TestStateEventTypedUnionAndOriginalRevision(t *testing.T) {
	network, err := DecodeNetworkInterfacesResult([]byte(testNetworkResult))
	if err != nil {
		t.Fatal(err)
	}
	network.Revision = ^uint64(0)
	values := []StateEvent{
		{Method: EventMethodSessionChanged, SessionChanged: &SessionChangedPayload{Session: validPublicSessionResult(), CleanupRequired: true}},
		{Method: EventMethodSessionRemoved, SessionRemoved: &SessionRemovedPayload{SessionID: "s", Revision: ^uint64(0)}},
		{Method: EventMethodNetworkChanged, NetworkChanged: &network},
	}
	for _, value := range values {
		raw, err := EncodeStateEvent(value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeStateEvent(raw)
		if err != nil || !reflect.DeepEqual(value, got) {
			t.Fatal("typed event lost paired facts", err)
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope) != 3 || string(envelope["kind"]) != `"event"` {
			t.Fatal("response-shaped event", err)
		}
		for _, bad := range []string{
			strings.Replace(string(raw), `"kind":"event"`, `"kind":"response"`, 1),
			strings.Replace(string(raw), `"kind":"event"`, `"kind":"event","id":"request"`, 1),
			strings.Replace(string(raw), `"method":`, `"method":"other","\u006dethod":`, 1),
			strings.Replace(string(raw), `"payload":`, `"Payload":`, 1),
			strings.Replace(string(raw), `"payload":`, `"extra":`, 1),
			string(raw) + `{}`, string(raw) + `!`,
		} {
			if _, err := DecodeStateEvent([]byte(bad)); err == nil {
				t.Fatal("invalid event envelope accepted")
			}
		}
		for _, token := range []string{"-0", "0", "-1", "1.0", "1e0", "18446744073709551616", "null", `"1"`} {
			old := `"revision":18446744073709551615`
			if value.SessionChanged != nil {
				old = `"revision":9007199254740993`
			}
			bad := strings.Replace(string(raw), old, `"revision":`+token, 1)
			if _, err := DecodeStateEvent([]byte(bad)); err == nil {
				t.Fatalf("noncanonical revision accepted %s", token)
			}
		}
	}
	for _, value := range []StateEvent{
		{}, {Method: EventMethodSessionChanged},
		{Method: EventMethodSessionRemoved, SessionRemoved: &SessionRemovedPayload{SessionID: "", Revision: 1}},
		{Method: EventMethodSessionChanged, SessionChanged: values[0].SessionChanged, SessionRemoved: values[1].SessionRemoved},
		{Method: EventMethodNetworkChanged, NetworkChanged: &NetworkInterfacesResult{Interfaces: []NetworkInterfaceResult{}}},
		{Method: "other", SessionRemoved: values[1].SessionRemoved},
	} {
		if _, err := EncodeStateEvent(value); err == nil {
			t.Fatal("invalid union encoded")
		}
	}
	raw, err := EncodeStateEvent(values[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `,"cleanupRequired":true`, ``, 1),
		strings.Replace(string(raw), `"cleanupRequired":true`, `"cleanupRequired":null`, 1),
		strings.Replace(string(raw), `"cleanupRequired":true`, `"cleanupRequired":"true"`, 1),
		strings.Replace(string(raw), `"cleanupRequired":true`, `"cleanupRequired":true,"surplus":false`, 1),
		strings.Replace(string(raw), `"accountName":"a"`, `"accountName":"\udfff"`, 1),
	} {
		if _, err := DecodeStateEvent([]byte(bad)); err == nil {
			t.Fatal("invalid changed payload accepted")
		}
	}
	values[0].SessionChanged.Session.DisplayName = strings.Repeat("a", StateFrameLimit)
	if _, err := EncodeStateEvent(values[0]); err == nil {
		t.Fatal("oversize event encoded")
	}
	removed := StateEvent{Method: EventMethodSessionRemoved, SessionRemoved: &SessionRemovedPayload{SessionID: "网😀", Revision: 9007199254740993}}
	raw, err = EncodeStateEvent(removed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeStateEvent(raw)
	if err != nil || !reflect.DeepEqual(got, removed) {
		t.Fatal("valid Unicode/integer lost", err)
	}
	removed.SessionRemoved.SessionID = string([]byte{0xff})
	if _, err := EncodeStateEvent(removed); err == nil {
		t.Fatal("malformed Go ID Unicode replaced")
	}
}
