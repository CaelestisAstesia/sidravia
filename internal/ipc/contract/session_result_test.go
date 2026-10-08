package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func validPublicSessionResult() SessionResult {
	return SessionResult{AuthenticationSessionID: "s", InstitutionProfileID: "i", InstitutionDisplayName: "Institution", AuthenticationProtocolID: "p", AccountName: "a", Intent: "maintain_authentication", State: "authenticated", Revision: 9007199254740993, UpdatedAt: "2026-10-07T01:02:03Z", ProtocolSocket: NetworkProtocolSocket{State: "not_observed"}}
}

func TestCompleteSessionResultStrictOriginalIntegers(t *testing.T) {
	value := validPublicSessionResult()
	value.Revision = ^uint64(0)
	now := value.UpdatedAt
	value.ProtocolSocket = NetworkProtocolSocket{State: "closed", RunGeneration: ^uint64(0), UpdatedAt: &now, LocalEndpoint: &NetworkEndpoint{Address: "127.0.0.1", Port: 1}, RemoteEndpoint: &NetworkEndpoint{Address: "255.255.255.255", Port: 2}}
	raw, err := MarshalSessionResult(value)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSessionResult(raw)
	if err != nil || got.Revision != value.Revision || got.ProtocolSocket.RunGeneration != value.ProtocolSocket.RunGeneration {
		t.Fatal("exact integer lost", err)
	}
	for _, token := range []string{"-0", "-1", "0", "1.0", "1e0", "18446744073709551616", "null", "\"1\"", "{}", "[]"} {
		malformed := strings.Replace(string(raw), `"revision":18446744073709551615`, `"revision":`+token, 1)
		if _, err := DecodeSessionResult([]byte(malformed)); err == nil {
			t.Fatalf("invalid revision accepted: %s", token)
		}
	}
	for _, malformed := range []string{strings.Replace(string(raw), `"protocolSocket":`, `"unknown":`, 1), strings.Replace(string(raw), `"revision":`, `"revision":1,"\u0072evision":`, 1), strings.Replace(string(raw), `"accountName":"a"`, `"accountName":"\ud800"`, 1), string(raw) + "{}", strings.Replace(string(raw), `"state":"closed"`, `"state":"open","runGeneration":0`, 1)} {
		if _, err := DecodeSessionResult([]byte(malformed)); err == nil {
			t.Fatal("invalid complete result accepted")
		}
	}
	for _, state := range []string{"not_observed", "open", "closed", "close_failed", "close_unconfirmed"} {
		value.ProtocolSocket.State = state
		if state == "not_observed" {
			value.ProtocolSocket.LocalEndpoint = nil
			value.ProtocolSocket.RemoteEndpoint = nil
		}
		if _, err := MarshalSessionResult(value); err != nil {
			t.Fatal(state, err)
		}
		if state == "not_observed" {
			value.ProtocolSocket.LocalEndpoint = &NetworkEndpoint{Address: "127.0.0.1", Port: 1}
			value.ProtocolSocket.RemoteEndpoint = &NetworkEndpoint{Address: "192.0.2.1", Port: 2}
		}
	}
}

func TestSessionListCleanupMembershipAndStartShareStrictResult(t *testing.T) {
	value := validPublicSessionResult()
	raw, err := MarshalSessionListResult(SessionListResult{Sessions: []SessionResult{value}, CleanupRequiredSessionIDs: []string{"s"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSessionListResult(raw)
	if err != nil || len(got.CleanupRequiredSessionIDs) != 1 {
		t.Fatal(err)
	}
	for _, ids := range []string{`null`, `["missing"]`, `[""]`, `["s","s"]`, `[1]`} {
		bad := strings.Replace(string(raw), `["s"]`, ids, 1)
		if _, err := DecodeSessionListResult([]byte(bad)); err == nil {
			t.Fatal("invalid cleanup IDs accepted", ids)
		}
	}
	if _, err := MarshalSessionListResult(SessionListResult{Sessions: []SessionResult{value, value}, CleanupRequiredSessionIDs: []string{}}); err == nil {
		t.Fatal("duplicate actors accepted")
	}
	if _, err := MarshalSessionListResult(SessionListResult{}); err == nil {
		t.Fatal("nil arrays accepted")
	}
	start, _ := json.Marshal(SessionStartResult{Outcome: "created", Session: value})
	if _, err := DecodeSessionStartResult(start); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSessionStartResult([]byte(strings.Replace(string(start), `"outcome":"created"`, `"outcome":"future"`, 1))); err == nil {
		t.Fatal("invalid outcome accepted")
	}
}

func TestSessionBindingDisplayNameCanBeEmptyButRemainsRequiredString(t *testing.T) {
	value := validPublicSessionResult()
	value.SelectedNetworkBinding = &SessionNetworkBinding{InterfaceID: "i", DisplayName: "", LocalIPv4Address: "192.0.2.10"}
	raw, err := MarshalSessionResult(value)
	if err != nil {
		t.Fatal("valid empty binding label rejected", err)
	}
	got, err := DecodeSessionResult(raw)
	if err != nil || got.SelectedNetworkBinding == nil || got.SelectedNetworkBinding.DisplayName != "" {
		t.Fatal("empty label fabricated or lost", err)
	}
	for _, replacement := range []string{`"displayName":null`, `"displayName":1`, `"displayName":true`, `"displayName":{}`, `"displayName":[]`, `"unknown":""`} {
		bad := strings.Replace(string(raw), `"selectedNetworkBinding":{"interfaceId":"i","displayName":""`, `"selectedNetworkBinding":{"interfaceId":"i",`+replacement, 1)
		if _, err := DecodeSessionResult([]byte(bad)); err == nil {
			t.Fatal("invalid binding label accepted", replacement)
		}
	}
	missing := strings.Replace(string(raw), `"selectedNetworkBinding":{"interfaceId":"i","displayName":"",`, `"selectedNetworkBinding":{"interfaceId":"i",`, 1)
	if _, err := DecodeSessionResult([]byte(missing)); err == nil {
		t.Fatal("missing binding label accepted")
	}
}
