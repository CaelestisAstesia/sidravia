package credentials

import (
	"errors"
	"strings"
	"testing"

	"sidravia/internal/daemon/persistence"
)

func TestStoreDocumentRoundTripIsDeterministic(t *testing.T) {
	credentials := map[CredentialID]AuthenticationCredential{
		"credential-b": {Username: "second-account", Password: "second-secret"},
		"credential-a": {Username: "first-account", Password: ""},
	}
	data, err := encodeStoreDocument(credentials)
	if err != nil {
		t.Fatalf("encodeStoreDocument() error = %v", err)
	}
	want := `{"schemaVersion":1,"credentials":[{"credentialId":"credential-a","username":"first-account","password":""},{"credentialId":"credential-b","username":"second-account","password":"second-secret"}]}`
	if string(data) != want {
		t.Fatalf("encodeStoreDocument() = %s, want exact deterministic document", data)
	}
	decoded, err := decodeStoreDocument(data)
	if err != nil {
		t.Fatalf("decodeStoreDocument() error = %v", err)
	}
	if len(decoded) != 2 || decoded["credential-a"].Username != "first-account" || decoded["credential-a"].Password != "" || decoded["credential-b"].Password != "second-secret" {
		t.Fatalf("decoded credential count or fields do not match")
	}
}

func TestDecodeStoreDocumentRejectsInvalidDocuments(t *testing.T) {
	validRecord := `{"credentialId":"credential-1","username":"account","password":"secret-value"}`
	tests := []struct {
		name string
		data string
		code persistence.FailureCode
	}{
		{name: "unknown root field", data: `{"schemaVersion":1,"credentials":[],"extra":true}`, code: persistence.FailureInvalidDocument},
		{name: "missing password", data: `{"schemaVersion":1,"credentials":[{"credentialId":"credential-1","username":"account"}]}`, code: persistence.FailureInvalidDocument},
		{name: "unsupported schema", data: `{"schemaVersion":2,"credentials":[]}`, code: persistence.FailureUnsupportedSchemaVersion},
		{name: "duplicate id", data: `{"schemaVersion":1,"credentials":[` + validRecord + `,` + validRecord + `]}`, code: persistence.FailureInvalidDocument},
		{name: "empty id", data: `{"schemaVersion":1,"credentials":[{"credentialId":"","username":"account","password":"secret-value"}]}`, code: persistence.FailureInvalidDocument},
		{name: "empty username", data: `{"schemaVersion":1,"credentials":[{"credentialId":"credential-1","username":"","password":"secret-value"}]}`, code: persistence.FailureInvalidDocument},
		{name: "damaged json", data: `{`, code: persistence.FailureInvalidDocument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeStoreDocument([]byte(test.data))
			if got := credentialsPersistenceFailureCode(t, err); got != test.code {
				t.Fatalf("failure code = %q, want %q", got, test.code)
			}
			for _, secret := range []string{"credential-1", "account", "secret-value"} {
				if err != nil && strings.Contains(err.Error(), secret) {
					t.Fatalf("public error leaked secret category %q", secret)
				}
			}
		})
	}
}

func TestEncodeStoreDocumentRejectsInvalidValuesWithoutLeakingSecrets(t *testing.T) {
	for _, credentials := range []map[CredentialID]AuthenticationCredential{
		{"": {Username: "private-account", Password: "private-secret"}},
		{"credential-private": {Username: "", Password: "private-secret"}},
	} {
		_, err := encodeStoreDocument(credentials)
		if got := credentialsPersistenceFailureCode(t, err); got != persistence.FailureInvalidArgument {
			t.Fatalf("failure code = %q, want invalid_argument", got)
		}
		for _, secret := range []string{"credential-private", "private-account", "private-secret"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("public error leaked secret category %q", secret)
			}
		}
	}
}

func credentialsPersistenceFailureCode(t *testing.T, err error) persistence.FailureCode {
	t.Helper()
	var failure *persistence.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error %T does not expose persistence.Failure", err)
	}
	return failure.Code()
}
