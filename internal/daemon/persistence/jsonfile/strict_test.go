package jsonfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/daemon/persistence"
)

type strictConfiguration struct {
	Enabled                 bool            `json:"enabled"`
	ProtocolContextOverride json.RawMessage `json:"protocolContextOverride"`
}

type strictDocument struct {
	Name          string              `json:"name"`
	Configuration strictConfiguration `json:"configuration"`
}

func TestReadLimitedAcceptsExactLimitAndRejectsOneByteOver(t *testing.T) {
	got, err := ReadLimited(strings.NewReader("abc"), 3)
	if err != nil || string(got) != "abc" {
		t.Fatalf("ReadLimited exact limit = %q, %v", got, err)
	}
	if _, err := ReadLimited(strings.NewReader("abcd"), 3); failureCode(t, err) != persistence.FailureSizeLimitExceeded {
		t.Fatalf("ReadLimited one byte over error code = %q", failureCode(t, err))
	}
	if _, err := ReadLimited(strings.NewReader(""), 0); failureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("ReadLimited invalid maximum error code = %q", failureCode(t, err))
	}
}

func TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "trailing value", data: `{"name":"one"} {}`},
		{name: "unknown field", data: `{"name":"one","extra":true}`},
		{name: "duplicate root key", data: `{"name":"one","name":"two"}`},
		{name: "wrong type", data: `{"name":false}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var destination strictDocument
			if err := DecodeStrict([]byte(test.data), &destination); failureCode(t, err) != persistence.FailureInvalidDocument {
				t.Fatalf("DecodeStrict error code = %q, want invalid_document", failureCode(t, err))
			}
		})
	}
}

func TestDecodeStrictRejectsNestedOwnedDuplicateKeys(t *testing.T) {
	data := []byte(`{"name":"one","configuration":{"enabled":true,"enabled":false}}`)
	var destination strictDocument
	if err := DecodeStrict(data, &destination); failureCode(t, err) != persistence.FailureInvalidDocument {
		t.Fatalf("DecodeStrict nested duplicate error code = %q", failureCode(t, err))
	}
}

func TestDecodeStrictSkipsOnlyTheExactOpaqueJSONPointer(t *testing.T) {
	allowed := []byte(`{"name":"one","configuration":{"protocolContextOverride":{"token":"one","token":"two"}}}`)
	var destination strictDocument
	if err := DecodeStrict(allowed, &destination, "/configuration/protocolContextOverride"); err != nil {
		t.Fatalf("DecodeStrict rejected duplicate within exact opaque pointer: %v", err)
	}

	siblingDuplicate := []byte(`{"name":"one","name":"two","configuration":{"protocolContextOverride":{"token":"one","token":"two"}}}`)
	if err := DecodeStrict(siblingDuplicate, &destination, "/configuration/protocolContextOverride"); failureCode(t, err) != persistence.FailureInvalidDocument {
		t.Fatalf("DecodeStrict sibling duplicate error code = %q", failureCode(t, err))
	}
}

func TestStrictJSONValidationRejectsInvalidUTF8(t *testing.T) {
	invalidDocument := []byte("{\"name\":\"\xff\"}")
	invalidFieldDocument := []byte("{\"field\":\"\xff\"}")
	invalidSchemaDocument := []byte("{\"schemaVersion\":1,\"payload\":\"\xff\"}")
	invalidOpaque := json.RawMessage("{\"payload\":\"\xff\"}")
	invalidRecord := json.RawMessage("{\"payload\":\"\xff\"}")
	var destination strictDocument
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "decode strict", err: DecodeStrict(invalidDocument, &destination)},
		{name: "require fields", err: RequireObjectFields(invalidFieldDocument, "field")},
		{name: "require schema", err: RequireSchemaVersion(invalidSchemaDocument, SchemaVersion1)},
		{name: "validate opaque", err: ValidateOpaqueObjectOrNull(invalidOpaque)},
		{name: "raw envelope", err: rawEnvelopeError(invalidRecord)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := failureCode(t, test.err); got != persistence.FailureInvalidDocument {
				t.Fatalf("failure code = %q, want invalid_document", got)
			}
		})
	}
}

func TestOpaqueAndRawValidationAcceptLargeJSONNumbers(t *testing.T) {
	data := []byte(`{"name":"one","configuration":{"protocolContextOverride":{"large":1e400}}}`)
	var destination strictDocument
	if err := DecodeStrict(data, &destination, "/configuration/protocolContextOverride"); err != nil {
		t.Fatalf("DecodeStrict rejected opaque 1e400: %v", err)
	}
	if err := ValidateOpaqueObjectOrNull(json.RawMessage(`{"large":1e400}`)); err != nil {
		t.Fatalf("ValidateOpaqueObjectOrNull rejected 1e400: %v", err)
	}
	if _, err := MarshalRawRecordEnvelope(SchemaVersion1, []json.RawMessage{json.RawMessage(`{"large":1e400}`)}); err != nil {
		t.Fatalf("MarshalRawRecordEnvelope rejected 1e400: %v", err)
	}
}

func TestRequireObjectFieldsDistinguishesMissingFromZeroValue(t *testing.T) {
	present := []byte(`{"enabled":false,"displayName":"","nullable":null}`)
	if err := RequireObjectFields(present, "enabled", "displayName", "nullable"); err != nil {
		t.Fatalf("RequireObjectFields rejected present zero/null fields: %v", err)
	}
	if err := RequireObjectFields([]byte(`{"enabled":false}`), "enabled", "displayName"); failureCode(t, err) != persistence.FailureInvalidDocument {
		t.Fatalf("RequireObjectFields missing-field error code = %q", failureCode(t, err))
	}
}

func TestRequireSchemaVersionAcceptsOnlyIntegerOne(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		code persistence.FailureCode
	}{
		{name: "missing", data: `{}`, code: persistence.FailureInvalidDocument},
		{name: "null", data: `{"schemaVersion":null}`, code: persistence.FailureInvalidDocument},
		{name: "string", data: `{"schemaVersion":"1"}`, code: persistence.FailureInvalidDocument},
		{name: "fractional", data: `{"schemaVersion":1.0}`, code: persistence.FailureInvalidDocument},
		{name: "negative", data: `{"schemaVersion":-1}`, code: persistence.FailureInvalidDocument},
		{name: "zero", data: `{"schemaVersion":0}`, code: persistence.FailureUnsupportedSchemaVersion},
		{name: "one", data: `{"schemaVersion":1}`, code: ""},
		{name: "two", data: `{"schemaVersion":2}`, code: persistence.FailureUnsupportedSchemaVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := RequireSchemaVersion([]byte(test.data), SchemaVersion1)
			if test.code == "" && err != nil {
				t.Fatalf("RequireSchemaVersion(%s) = %v, want nil", test.data, err)
			}
			if test.code != "" && failureCode(t, err) != test.code {
				t.Fatalf("RequireSchemaVersion error code = %q, want %q", failureCode(t, err), test.code)
			}
		})
	}
}

func TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		data json.RawMessage
		want persistence.FailureCode
	}{
		{name: "object", data: json.RawMessage(`{"token":"value"}`)},
		{name: "null", data: json.RawMessage(`null`)},
		{name: "scalar", data: json.RawMessage(`true`), want: persistence.FailureInvalidDocument},
		{name: "array", data: json.RawMessage(`[]`), want: persistence.FailureInvalidDocument},
		{name: "invalid", data: json.RawMessage(`{`), want: persistence.FailureInvalidDocument},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateOpaqueObjectOrNull(test.data)
			if test.want == "" && err != nil {
				t.Fatalf("ValidateOpaqueObjectOrNull(%s) = %v, want nil", test.data, err)
			}
			if test.want != "" && failureCode(t, err) != test.want {
				t.Fatalf("ValidateOpaqueObjectOrNull error code = %q, want %q", failureCode(t, err), test.want)
			}
		})
	}
}

func TestMarshalDeterministicUsesOwnedStructOrderWithoutNewline(t *testing.T) {
	value := struct {
		SchemaVersion uint64 `json:"schemaVersion"`
		DisplayName   string `json:"displayName"`
	}{SchemaVersion: SchemaVersion1, DisplayName: "Example"}
	got, err := MarshalDeterministic(value)
	if err != nil {
		t.Fatalf("MarshalDeterministic() error = %v", err)
	}
	if want := `{"schemaVersion":1,"displayName":"Example"}`; string(got) != want {
		t.Fatalf("MarshalDeterministic() = %s, want %s", got, want)
	}
	if bytes.HasSuffix(got, []byte("\n")) {
		t.Fatal("MarshalDeterministic added a trailing newline")
	}
}

func TestMarshalRawRecordEnvelopePreservesEveryRawElementByte(t *testing.T) {
	records := []json.RawMessage{json.RawMessage(" {\"id\":1} "), json.RawMessage("\n[true, false]\t")}
	got, err := MarshalRawRecordEnvelope(SchemaVersion1, records)
	if err != nil {
		t.Fatalf("MarshalRawRecordEnvelope() error = %v", err)
	}
	want := "{\"schemaVersion\":1,\"records\":[" + string(records[0]) + "," + string(records[1]) + "]}"
	if string(got) != want {
		t.Fatalf("MarshalRawRecordEnvelope() = %q, want exact raw preservation %q", got, want)
	}
}

func TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement(t *testing.T) {
	overLimit := json.RawMessage(append(bytes.Repeat([]byte(" "), int(RawRecordSizeLimit)), []byte("null")...))
	for _, test := range []struct {
		name    string
		records []json.RawMessage
		want    persistence.FailureCode
	}{
		{name: "oversized", records: []json.RawMessage{overLimit}, want: persistence.FailureSizeLimitExceeded},
		{name: "multiple values", records: []json.RawMessage{json.RawMessage(`{} {}`)}, want: persistence.FailureInvalidDocument},
		{name: "empty", records: []json.RawMessage{json.RawMessage("")}, want: persistence.FailureInvalidDocument},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := MarshalRawRecordEnvelope(SchemaVersion1, test.records)
			if got := failureCode(t, err); got != test.want {
				t.Fatalf("failure code = %q, want %q", got, test.want)
			}
		})
	}
}

func rawEnvelopeError(record json.RawMessage) error {
	_, err := MarshalRawRecordEnvelope(SchemaVersion1, []json.RawMessage{record})
	return err
}

func failureCode(t *testing.T, err error) persistence.FailureCode {
	t.Helper()
	if err == nil {
		return ""
	}
	var failure *persistence.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error %T does not expose a persistence Failure", err)
	}
	return failure.Code()
}
