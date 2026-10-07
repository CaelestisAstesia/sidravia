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

func TestDiagnosticExportMaximumRowsWithUsefulSafeCategories(t *testing.T) {
	value := DiagnosticsExportResult{SchemaVersion: 1, GeneratedAt: "2026-10-07T01:02:03Z", ProductVersion: "v", BuildID: "b", OperatingSystem: "windows", Architecture: "amd64", Catalog: DiagnosticExportCatalog{StorageProtection: "protected"}, Sessions: DiagnosticExportSessions{TotalCount: 64, Items: []DiagnosticExportSession{}}}
	for i := 0; i < 64; i++ {
		value.Sessions.Items = append(value.Sessions.Items, DiagnosticExportSession{State: "waiting_before_retry", Intent: "maintain_authentication", ReasonCode: "runtime_definition_unavailable", ProtocolSocketState: "close_unconfirmed", FailureCategory: "runtime_definition_unavailable", RecoveryRecommendation: "block_until_explicit_restart_or_relevant_input_change", CleanupRequired: true})
	}
	raw, err := MarshalDiagnosticsExportResult(value)
	if err != nil || len(raw) > MaximumDiagnosticExportBytes {
		t.Fatalf("valid maximum rows exceed cap: %d %v", len(raw), err)
	}
	decoded, err := DecodeDiagnosticsExportResult(raw)
	if err != nil || len(decoded.Sessions.Items) != 64 || !decoded.Sessions.Items[63].CleanupRequired {
		t.Fatal("maximum safe facts lost")
	}
}
