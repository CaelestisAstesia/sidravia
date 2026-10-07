package contract

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNetworkDiagnosisRejectsOriginalInvalidUTF8AndNestedUnicode(t *testing.T) {
	for _, wire := range [][]byte{
		append([]byte(`{"configurationId":"`), append([]byte{0xff}, []byte(`"}`)...)...),
		[]byte(`{"configurationId":"c","\ud800":true}`),
		[]byte(`{"configurationId":"c","probe":false,"other":{"s":"\ud800"}}`),
	} {
		if _, err := DecodeNetworkDiagnosePayload(wire); err == nil {
			t.Fatal("invalid original accepted")
		}
	}
	wire := []byte(`{"observedAt":"2026-10-07T01:02:03Z","selectionBasis":"os_route_proposal","status":"unsupported","unsupportedReason":"protocol"}`)
	if _, err := DecodeNetworkDiagnoseResult(wire); err != nil {
		t.Fatal(err)
	}
	malformed := bytes.Replace(wire, []byte("protocol"), []byte{0xff}, 1)
	if _, err := DecodeNetworkDiagnoseResult(malformed); err == nil {
		t.Fatal("invalid UTF8 result accepted")
	}
}
func TestNetworkDiagnosisMarshalValidatesWholeProjection(t *testing.T) {
	value := NetworkDiagnoseResult{ObservedAt: "2026-10-07T01:02:03Z", SelectionBasis: "os_route_proposal", Status: "unsupported"}
	if _, err := MarshalNetworkDiagnoseResult(value); err == nil {
		t.Fatal("incomplete typed result emitted")
	}
	reason := "protocol"
	value.UnsupportedReason = &reason
	raw, err := MarshalNetworkDiagnoseResult(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatal("placeholder facts emitted")
	}
	for _, wire := range []string{`{"configurationId":"c"}`, `{"sessionId":"s","probe":false}`} {
		value, err := DecodeNetworkDiagnosePayload([]byte(wire))
		if err != nil || value.Probe {
			t.Fatal("read-only selector rejected")
		}
	}
}
