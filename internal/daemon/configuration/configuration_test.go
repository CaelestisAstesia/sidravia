package configuration

import (
	"net/netip"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
)

func TestConfigurationValidate(t *testing.T) {
	valid := Configuration{
		ConfigurationID:      "configuration-1",
		InstitutionProfileID: "profile-1",
		Username:             "user-1",
		NetworkBindingPolicy: NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
	}

	tests := []struct {
		name          string
		configuration Configuration
		wantError     bool
	}{
		{name: "valid", configuration: valid},
		{name: "missing configuration id", configuration: Configuration{InstitutionProfileID: valid.InstitutionProfileID, Username: valid.Username, NetworkBindingPolicy: valid.NetworkBindingPolicy}, wantError: true},
		{name: "missing profile id", configuration: Configuration{ConfigurationID: valid.ConfigurationID, Username: valid.Username, NetworkBindingPolicy: valid.NetworkBindingPolicy}, wantError: true},
		{name: "missing username", configuration: Configuration{ConfigurationID: valid.ConfigurationID, InstitutionProfileID: valid.InstitutionProfileID, NetworkBindingPolicy: valid.NetworkBindingPolicy}, wantError: true},
		{name: "unsupported network binding policy", configuration: Configuration{ConfigurationID: valid.ConfigurationID, InstitutionProfileID: valid.InstitutionProfileID, Username: valid.Username, NetworkBindingPolicy: NetworkBindingPolicy{Mode: "unsupported"}}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.configuration.Validate()
			if (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestConfigurationCloneOwnsProtocolContextOverride(t *testing.T) {
	original := Configuration{
		ConfigurationID:         "configuration-1",
		InstitutionProfileID:    "profile-1",
		Username:                "user-1",
		NetworkBindingPolicy:    NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
		ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{"network":"campus"}`),
	}

	cloned := original.Clone()
	cloned.ProtocolContextOverride[0] = '['
	if original.ProtocolContextOverride[0] == '[' {
		t.Fatal("Clone() shared ProtocolContextOverride storage with the original")
	}
}

func TestExplicitNetworkBindingPolicyValidation(t *testing.T) {
	valid := NetworkBindingPolicy{Mode: ExplicitInterfaceAndLocalIPv4, InterfaceID: "Loopback-真实", LocalIPv4Address: netip.MustParseAddr("127.0.0.1")}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", " leading", "trailing\u00a0", "a\n", "a\u0085b", string([]byte{0xff})} {
		v := valid
		v.InterfaceID = id
		if v.Validate() == nil {
			t.Fatal("invalid id accepted")
		}
	}
	for _, ip := range []string{"0.0.0.0", "224.0.0.1", "::ffff:127.0.0.1", "::1"} {
		v := valid
		v.LocalIPv4Address = netip.MustParseAddr(ip)
		if v.Validate() == nil {
			t.Fatal("invalid address accepted")
		}
	}
	v := valid
	v.Mode = AutomaticallySelectLatestAvailable
	if v.Validate() == nil {
		t.Fatal("mixed automatic target accepted")
	}
}
