package contract

import (
	"strings"
	"testing"
)

func TestConfigurationAvailabilityOwnsMetadataAndStrictGrammar(t *testing.T) {
	value := ConfigurationResult{ConfigurationID: "c", InstitutionProfileID: "i", InstitutionDisplayName: "Institution", AuthenticationProtocolID: "p", Username: "u", StorageProtection: "protected", NetworkBindingPolicy: NetworkBindingPolicy{Mode: "automatically_select_latest_available"}, RuntimeAvailability: "available"}
	for _, status := range []string{"available", "protocol_unavailable", "override_invalid", "profile_unavailable"} {
		value.RuntimeAvailability = status
		if status == "profile_unavailable" {
			value.InstitutionDisplayName = ""
			value.AuthenticationProtocolID = ""
		}
		raw, err := MarshalConfigurationResult(value)
		if err != nil {
			t.Fatal(status, err)
		}
		got, err := DecodeConfigurationResult(raw)
		if err != nil || got.RuntimeAvailability != status || got.CredentialStored {
			t.Fatal("read facts lost", err)
		}
		for _, bad := range []string{strings.Replace(string(raw), `"runtimeAvailability":`, `"unknown":`, 1), strings.Replace(string(raw), `"username":"u"`, `"username":"u","\u0075sername":"u"`, 1), strings.Replace(string(raw), `"username":"u"`, `"username":"\ud800"`, 1), strings.Replace(string(raw), `"credentialStored":false`, `"credentialStored":null`, 1), string(raw) + "{}"} {
			if _, err := DecodeConfigurationResult([]byte(bad)); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		}
		if status == "profile_unavailable" {
			value.InstitutionDisplayName = "invented"
		} else {
			value.InstitutionDisplayName = ""
		}
		if _, err := MarshalConfigurationResult(value); err == nil {
			t.Fatal("invalid conditional metadata accepted", status)
		}
		value.InstitutionDisplayName = "Institution"
	}
	value.RuntimeAvailability = "future"
	if _, err := MarshalConfigurationResult(value); err == nil {
		t.Fatal("unknown availability accepted")
	}
	if _, err := DecodeConfigurationListResult([]byte(`{"storageProtection":"protected","configurations":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeConfigurationListResult([]byte(`{"storageProtection":"protected","configurations":null}`)); err == nil {
		t.Fatal("null configurations accepted")
	}
}
