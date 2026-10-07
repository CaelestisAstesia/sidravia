package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnosticExportRawUnicodeAndValidatedMarshal(t *testing.T) {
	for _, raw := range [][]byte{[]byte("{\"x\":\"" + string([]byte{0xff}) + "\"}"), []byte(`{"\ud800":1}`), []byte(`{"a":1,"\u0061":1}`)} {
		if err := DecodeDiagnosticsExportPayload(raw); err == nil {
			t.Fatal("invalid raw payload accepted")
		}
	}
	valid := DiagnosticsExportResult{SchemaVersion: 1, GeneratedAt: "2026-10-07T01:02:03Z", ProductVersion: "1.0.0+safe", BuildID: "b", OperatingSystem: "other", Architecture: "other", Network: DiagnosticExportNetwork{Available: false}, Catalog: DiagnosticExportCatalog{StorageProtection: "unprotected"}, Sessions: DiagnosticExportSessions{Items: []DiagnosticExportSession{}}}
	raw, err := MarshalDiagnosticsExportResult(valid)
	if err != nil {
		t.Fatal(err)
	}
	exact := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), MaximumDiagnosticExportBytes-len(raw))...)
	if _, err := DecodeDiagnosticsExportResult(exact); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDiagnosticsExportResult(append(exact, ' ')); err == nil {
		t.Fatal("oversize accepted")
	}
	invalid := valid
	invalid.Sessions.Items = nil
	if _, err := MarshalDiagnosticsExportResult(invalid); err == nil {
		t.Fatal("marshal bypassed null validation")
	}
	invalid = valid
	invalid.BuildID = "secret/path"
	if _, err := MarshalDiagnosticsExportResult(invalid); err == nil {
		t.Fatal("marshal bypassed safe metadata")
	}
	invalidUTF8 := bytes.Replace(raw, []byte(`"b"`), []byte{'"', 0xff, '"'}, 1)
	if _, err := DecodeDiagnosticsExportResult(invalidUTF8); err == nil {
		t.Fatal("invalid utf8 result accepted")
	}
	for _, metadata := range []string{"A-z_1.2+3", strings.Repeat("a", 128)} {
		if !ValidDiagnosticMetadata(metadata) {
			t.Fatal("safe metadata rejected")
		}
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if len(value) != 9 {
		t.Fatal("unexpected allowlist")
	}
}
