package configuration

import (
	"errors"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
)

func TestCatalogDocumentRoundTripIsDeterministic(t *testing.T) {
	records := map[ConfigurationID]catalogRecord{
		"configuration-b": {
			configuration: Configuration{
				ConfigurationID:         "configuration-b",
				DisplayName:             "Second",
				InstitutionProfileID:    "profile-b",
				Username:                "user-b",
				NetworkBindingPolicy:    NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
				ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{"server":"b"}`),
			},
			password: "password-b",
		},
		"configuration-a": {
			configuration: Configuration{
				ConfigurationID:      "configuration-a",
				DisplayName:          "First",
				InstitutionProfileID: "profile-a",
				Username:             "user-a",
				NetworkBindingPolicy: NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
			},
			password: "",
		},
	}
	data, err := encodeCatalogDocument(records)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":2,"configurations":[{"configurationId":"configuration-a","displayName":"First","institutionProfileId":"profile-a","username":"user-a","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null},{"configurationId":"configuration-b","displayName":"Second","institutionProfileId":"profile-b","username":"user-b","password":"password-b","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":{"server":"b"}}]}`
	if string(data) != want {
		t.Fatalf("encoded document differs: %s", data)
	}
	decoded, err := decodeCatalogDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded["configuration-a"].password != "" || decoded["configuration-b"].password != "password-b" {
		t.Fatal("passwords did not round trip")
	}
	if got := string(decoded["configuration-b"].configuration.ProtocolContextOverride); got != `{"server":"b"}` {
		t.Fatalf("override = %s", got)
	}
}

func TestDecodeCatalogDocumentAcceptsOpaqueObjectInternals(t *testing.T) {
	data := []byte(`{"schemaVersion":2,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","username":"user","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":{"token":"one","token":"two"}}]}`)
	records, err := decodeCatalogDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(records["configuration-1"].configuration.ProtocolContextOverride); got != `{"token":"one","token":"two"}` {
		t.Fatalf("opaque override = %s", got)
	}
}

func TestDecodeCatalogDocumentRejectsInvalidDocuments(t *testing.T) {
	valid := `{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","username":"user","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null}`
	tests := []struct {
		name string
		data string
		code persistence.FailureCode
	}{
		{"unknown root", `{"schemaVersion":2,"configurations":[],"extra":true}`, persistence.FailureInvalidDocument},
		{"missing field", `{"schemaVersion":2,"configurations":[{"configurationId":"configuration-1"}]}`, persistence.FailureInvalidDocument},
		{"old schema", `{"schemaVersion":1,"configurations":[]}`, persistence.FailureUnsupportedSchemaVersion},
		{"future schema", `{"schemaVersion":3,"configurations":[]}`, persistence.FailureUnsupportedSchemaVersion},
		{"duplicate id", `{"schemaVersion":2,"configurations":[` + valid + `,` + valid + `]}`, persistence.FailureInvalidDocument},
		{"invalid id", `{"schemaVersion":2,"configurations":[{"configurationId":"Invalid","displayName":"","institutionProfileId":"p","username":"u","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null}]}`, persistence.FailureInvalidDocument},
		{"trailing", `{"schemaVersion":2,"configurations":[]} {}`, persistence.FailureInvalidDocument},
		{"null", `null`, persistence.FailureInvalidDocument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeCatalogDocument([]byte(test.data))
			if got := configurationPersistenceFailureCode(t, err); got != test.code {
				t.Fatalf("code = %q, want %q", got, test.code)
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
