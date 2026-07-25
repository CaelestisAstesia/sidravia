package configuration

import (
	"errors"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/persistence"
)

func TestCatalogDocumentRoundTripIsDeterministic(t *testing.T) {
	configurations := map[ConfigurationID]Configuration{
		"configuration-b": {
			ConfigurationID:         "configuration-b",
			DisplayName:             "Second",
			InstitutionProfileID:    "profile-b",
			CredentialID:            credentials.CredentialID("credential-b"),
			NetworkBindingPolicy:    NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
			ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{"server":"b"}`),
		},
		"configuration-a": {
			ConfigurationID:      "configuration-a",
			DisplayName:          "First",
			InstitutionProfileID: "profile-a",
			CredentialID:         credentials.CredentialID("credential-a"),
			NetworkBindingPolicy: NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
		},
	}

	data, err := encodeCatalogDocument(configurations)
	if err != nil {
		t.Fatalf("encodeCatalogDocument() error = %v", err)
	}
	want := `{"schemaVersion":1,"configurations":[{"configurationId":"configuration-a","displayName":"First","institutionProfileId":"profile-a","credentialId":"credential-a","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null},{"configurationId":"configuration-b","displayName":"Second","institutionProfileId":"profile-b","credentialId":"credential-b","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":{"server":"b"}}]}`
	if string(data) != want {
		t.Fatalf("encodeCatalogDocument() = %s, want %s", data, want)
	}

	decoded, err := decodeCatalogDocument(data)
	if err != nil {
		t.Fatalf("decodeCatalogDocument() error = %v", err)
	}
	if len(decoded) != 2 || decoded["configuration-a"].DisplayName != "First" || string(decoded["configuration-b"].ProtocolContextOverride) != `{"server":"b"}` {
		t.Fatalf("decoded configurations = %#v", decoded)
	}
	decodedB := decoded["configuration-b"]
	decodedB.ProtocolContextOverride[0] = '['
	if configurations["configuration-b"].ProtocolContextOverride[0] == '[' {
		t.Fatal("codec result shared protocol override storage with input")
	}
}

func TestDecodeCatalogDocumentAcceptsOpaqueObjectInternals(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","credentialId":"credential-1","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":{"token":"one","token":"two"}}]}`)
	configurations, err := decodeCatalogDocument(data)
	if err != nil {
		t.Fatalf("decodeCatalogDocument() error = %v", err)
	}
	if got := string(configurations["configuration-1"].ProtocolContextOverride); got != `{"token":"one","token":"two"}` {
		t.Fatalf("opaque override = %s", got)
	}
}

func TestDecodeCatalogDocumentRejectsInvalidDocuments(t *testing.T) {
	validRecord := `{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","credentialId":"credential-1","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null}`
	tests := []struct {
		name string
		data string
		code persistence.FailureCode
	}{
		{name: "unknown root field", data: `{"schemaVersion":1,"configurations":[],"extra":true}`, code: persistence.FailureInvalidDocument},
		{name: "missing record field", data: `{"schemaVersion":1,"configurations":[{"configurationId":"configuration-1"}]}`, code: persistence.FailureInvalidDocument},
		{name: "unsupported schema", data: `{"schemaVersion":2,"configurations":[]}`, code: persistence.FailureUnsupportedSchemaVersion},
		{name: "duplicate id", data: `{"schemaVersion":1,"configurations":[` + validRecord + `,` + validRecord + `]}`, code: persistence.FailureInvalidDocument},
		{name: "invalid model", data: `{"schemaVersion":1,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","credentialId":"credential-1","networkBindingPolicy":{"mode":"unsupported"},"protocolContextOverride":null}]}`, code: persistence.FailureInvalidDocument},
		{name: "scalar override", data: `{"schemaVersion":1,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","credentialId":"credential-1","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":true}]}`, code: persistence.FailureInvalidDocument},
		{name: "damaged json", data: `{`, code: persistence.FailureInvalidDocument},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeCatalogDocument([]byte(test.data))
			if got := configurationPersistenceFailureCode(t, err); got != test.code {
				t.Fatalf("failure code = %q, want %q", got, test.code)
			}
		})
	}
}

func configurationPersistenceFailureCode(t *testing.T, err error) persistence.FailureCode {
	t.Helper()
	var failure *persistence.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error %T does not expose persistence.Failure", err)
	}
	return failure.Code()
}
