package contract

import (
	"strconv"
	"strings"
	"testing"
)

const businessVector = `{"sessionId":"s","runGeneration":9007199254740993,"revision":18446744073709551615,"current":true,"statisticsSupported":true,"textSupported":true,"statistics":{"receivedAt":"2026-11-14T01:02:03Z","source":"server_login","scope":"server_billing_history","trafficBytes":0,"balanceCents":null},"messages":[{"id":"1","receivedAt":"2026-11-14T01:02:03Z","text":"Hello"}],"updatedAt":"2026-11-14T01:02:03Z"}`

func TestBusinessIndependentVectorAndBounds(t *testing.T) {
	v, err := DecodeBusinessResult([]byte(businessVector))
	if err != nil {
		t.Fatal(err)
	}
	if v.Revision != ^uint64(0) || v.RunGeneration != 9007199254740993 || v.Statistics.TrafficBytes == nil || *v.Statistics.TrafficBytes != 0 || v.Statistics.BalanceCents != nil {
		t.Fatal("lossy decode")
	}
	if _, err := MarshalBusinessResult(v); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(businessVector, `"current":true`, `"current":null`, 1), strings.Replace(businessVector, `"balanceCents":null`, `"balanceCents":2147483648`, 1), strings.Replace(businessVector, `"trafficBytes":0`, `"trafficBytes":4398046510081`, 1), strings.Replace(businessVector, `"messages":[`, `"messages":null,"extra":[`, 1), strings.Replace(businessVector, `"Hello"`, `"\u001bhello"`, 1), strings.Replace(businessVector, `"Hello"`, `"\ud800"`, 1), strings.Replace(businessVector, `"scope":"server_billing_history"`, `"scope":"monthly"`, 1), strings.Replace(businessVector, `"textSupported":true`, `"textSupported":false`, 1), strings.Replace(businessVector, `"runGeneration":9007199254740993`, `"runGeneration":0`, 1), strings.Replace(businessVector, `"statisticsSupported":true,`, ``, 1)} {
		if _, err := DecodeBusinessResult([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	v.Messages = nil
	v.Statistics = nil
	if _, err := MarshalBusinessResult(v); err == nil {
		t.Fatal("null list accepted")
	}
	v.Messages = []BusinessMessage{}
	if _, err := MarshalBusinessResult(v); err != nil {
		t.Fatal(err)
	}
	// JSON's worst per-byte escaping still fits the transport envelope.
	for i := 0; i < 16; i++ {
		v.Messages = append(v.Messages, BusinessMessage{ID: string(rune('a' + i)), ReceivedAt: "2026-11-14T01:02:03Z", Text: strings.Repeat("<", 512)})
	}
	for i := range v.Messages {
		v.Messages[i].ID = strconv.Itoa(i + 1)
	}
	data, err := MarshalBusinessResult(v)
	if err != nil || len(data) > BusinessResultLimit {
		t.Fatalf("bounded response %d %v", len(data), err)
	}
	v.Messages = append(v.Messages, v.Messages[0])
	if _, err := MarshalBusinessResult(v); err == nil {
		t.Fatal("unbounded list")
	}
}
