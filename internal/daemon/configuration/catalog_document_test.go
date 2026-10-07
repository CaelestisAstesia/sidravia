package configuration

import (
	"errors"
	"strings"
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
	want := `{"schemaVersion":4,"configurations":[{"configurationId":"configuration-a","displayName":"First","institutionProfileId":"profile-a","username":"user-a","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null,"autoLogin":false,"autoReconnect":false},{"configurationId":"configuration-b","displayName":"Second","institutionProfileId":"profile-b","username":"user-b","password":"password-b","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":{"server":"b"},"autoLogin":false,"autoReconnect":false}]}`
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
		{"future schema", `{"schemaVersion":5,"configurations":[]}`, persistence.FailureUnsupportedSchemaVersion},
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

func TestDecodeCatalogDocumentSchemaV2DefaultsAutoLoginFalseAutoReconnectTrue(t *testing.T) {
	data := []byte(`{"schemaVersion":2,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","username":"user","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null}]}`)
	records, err := decodeCatalogDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	got := records["configuration-1"].configuration
	if got.AutoLogin || !got.AutoReconnect {
		t.Fatalf("schema-2 defaults: autoLogin=%v, autoReconnect=%v", got.AutoLogin, got.AutoReconnect)
	}
}

func TestDecodeCatalogDocumentSchemaV3RequiresBothBooleans(t *testing.T) {
	base := `{"schemaVersion":3,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","username":"user","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null`
	tests := []struct {
		name string
		data string
	}{
		{"missing autoLogin", base + `,"autoReconnect":false}]}`},
		{"missing autoReconnect", base + `,"autoLogin":false}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeCatalogDocument([]byte(test.data)); err == nil {
				t.Fatal("accepted schema-3 document with missing boolean")
			}
		})
	}
}

func TestEncodeCatalogDocumentSchemaV3RoundTrip(t *testing.T) {
	records := map[ConfigurationID]catalogRecord{
		"configuration-1": {
			configuration: Configuration{
				ConfigurationID:      "configuration-1",
				DisplayName:          "Campus",
				InstitutionProfileID: "profile-1",
				Username:             "user",
				NetworkBindingPolicy: NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
				AutoLogin:            true,
				AutoReconnect:        false,
			},
			password: "secret",
		},
	}
	data, err := encodeCatalogDocument(records)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCatalogDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded["configuration-1"].configuration
	if !got.AutoLogin || got.AutoReconnect {
		t.Fatalf("round-trip: autoLogin=%v, autoReconnect=%v", got.AutoLogin, got.AutoReconnect)
	}
}

func TestDecodeCatalogDocumentRejectsMultipleAutoLoginTrue(t *testing.T) {
	makeEntry := func(id string) string {
		return `{"configurationId":"` + id + `","displayName":"Campus","institutionProfileId":"profile-1","username":"user","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null,"autoLogin":true,"autoReconnect":false}`
	}
	data := []byte(`{"schemaVersion":3,"configurations":[` + makeEntry("configuration-a") + `,` + makeEntry("configuration-b") + `]}`)
	if _, err := decodeCatalogDocument(data); err == nil {
		t.Fatal("accepted document with more than one autoLogin=true")
	}
}

func TestCatalogStrictPolicyMigrationAndSchema4(t *testing.T) {
	base := `{"schemaVersion":3,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","username":"user","password":"","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null,"autoLogin":false,"autoReconnect":true}]}`
	explicit := `{"mode":"explicit_interface_and_local_ipv4","interfaceId":"lo","localIpv4Address":"127.0.0.1"}`
	for _, schema := range []string{"2", "3", "4"} {
		data := strings.Replace(base, `"schemaVersion":3`, `"schemaVersion":`+schema, 1)
		if schema == "2" {
			data = strings.Replace(data, `,"autoLogin":false,"autoReconnect":true`, "", 1)
		}
		records, err := decodeCatalogDocument([]byte(data))
		if err != nil || records["configuration-1"].configuration.NetworkBindingPolicy.Mode != AutomaticallySelectLatestAvailable {
			t.Fatal("automatic migration failed", err)
		}
		changed := strings.Replace(data, `{"mode":"automatically_select_latest_available"}`, explicit, 1)
		_, err = decodeCatalogDocument([]byte(changed))
		if (err == nil) != (schema == "4") {
			t.Fatal("schema-specific explicit policy contract differs")
		}
		nullTarget := strings.Replace(data, `{"mode":"automatically_select_latest_available"}`, `{"mode":"automatically_select_latest_available","interfaceId":null}`, 1)
		if _, err = decodeCatalogDocument([]byte(nullTarget)); err == nil {
			t.Fatal("mixed old policy accepted")
		}
	}
}
