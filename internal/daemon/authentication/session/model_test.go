package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	profile "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	environment "sidravia/internal/daemon/environment"
)

func TestRuntimeDefinitionValidateRejectsMismatchedProtocolFactory(t *testing.T) {
	definition := validRuntimeDefinition(t)
	definition.InstitutionProfile.AuthenticationProtocolID = "expected"
	definition.AuthenticationProtocolFactory = stubFactory{protocolID: "actual"}

	err := definition.Validate()

	if err == nil {
		t.Fatal("expected protocol ID mismatch")
	}
}

func TestRuntimeDefinitionValidateSanitizesFactoryValidationErrors(t *testing.T) {
	const factoryValidationSecret = "factory-validation-secret"

	tests := []struct {
		name    string
		factory stubFactory
		want    string
	}{
		{
			name: "institution protocol configuration",
			factory: stubFactory{
				protocolID:                    "test-protocol",
				institutionConfigurationError: errors.New(factoryValidationSecret),
			},
			want: "institution protocol configuration is invalid",
		},
		{
			name: "protocol context override",
			factory: stubFactory{
				protocolID:                   "test-protocol",
				protocolContextOverrideError: errors.New(factoryValidationSecret),
			},
			want: "protocol context override is invalid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validRuntimeDefinition(t)
			definition.AuthenticationProtocolFactory = test.factory

			err := definition.Validate()

			if err == nil {
				t.Fatal("expected validation error")
			}
			if strings.Contains(err.Error(), factoryValidationSecret) {
				t.Fatalf("validation error exposes factory detail: %v", err)
			}
			if err.Error() != test.want {
				t.Errorf("Validate() error = %q, want %q", err, test.want)
			}
		})
	}
}

func TestRuntimeDefinitionValidate(t *testing.T) {
	tests := []struct {
		name      string
		wantValid bool
		mutate    func(*RuntimeDefinition)
	}{
		{
			name:      "accepts a valid definition",
			wantValid: true,
			mutate:    func(*RuntimeDefinition) {},
		},
		{
			name: "rejects an empty session ID",
			mutate: func(definition *RuntimeDefinition) {
				definition.Configuration.AuthenticationSessionID = ""
			},
		},
		{
			name: "rejects an empty profile ID",
			mutate: func(definition *RuntimeDefinition) {
				definition.Configuration.InstitutionProfileID = ""
			},
		},
		{
			name:      "accepts an empty credential ID for one-shot",
			wantValid: true,
			mutate: func(definition *RuntimeDefinition) {
				definition.Configuration.CredentialID = ""
			},
		},
		{
			name: "rejects an empty protocol ID",
			mutate: func(definition *RuntimeDefinition) {
				definition.InstitutionProfile.AuthenticationProtocolID = ""
			},
		},
		{
			name: "rejects an unsupported network binding policy",
			mutate: func(definition *RuntimeDefinition) {
				definition.Configuration.NetworkBindingPolicy.Mode = "manual"
			},
		},
		{
			name: "rejects mismatched profile IDs",
			mutate: func(definition *RuntimeDefinition) {
				definition.InstitutionProfile.InstitutionProfileID = "another-profile"
			},
		},
		{
			name: "rejects invalid institution protocol configuration",
			mutate: func(definition *RuntimeDefinition) {
				definition.InstitutionProfile.InstitutionProtocolConfiguration = []byte("invalid")
			},
		},
		{
			name: "rejects invalid protocol context override",
			mutate: func(definition *RuntimeDefinition) {
				definition.Configuration.ProtocolContextOverride = []byte("invalid")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validRuntimeDefinition(t)
			test.mutate(&definition)

			err := definition.Validate()

			if test.wantValid {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected validation error")
			}
			if strings.Contains(err.Error(), "secret-password") {
				t.Fatalf("validation error exposes a secret: %v", err)
			}
		})
	}
}

func TestRuntimeDefinitionAccountLabelMasksUnicodeRunes(t *testing.T) {
	tests := []struct {
		username string
		want     string
	}{
		{username: "", want: ""},
		{username: "a", want: "*"},
		{username: "ab", want: "a*"},
		{username: "alice", want: "a***e"},
		{username: "你好世界", want: "你**界"},
	}

	for _, test := range tests {
		t.Run(test.username, func(t *testing.T) {
			definition := validRuntimeDefinition(t)
			definition.AuthenticationCredential.Username = test.username

			if got := definition.AccountLabel(); got != test.want {
				t.Errorf("AccountLabel() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStateReasonCodesAreStable(t *testing.T) {
	if StateReasonCodeNetworkUnavailable != "network_unavailable" {
		t.Errorf("network unavailable code = %q", StateReasonCodeNetworkUnavailable)
	}
	if StateReasonCodeProtocolRunCreationFailed != "protocol_run_creation_failed" {
		t.Errorf("protocol run creation failure code = %q", StateReasonCodeProtocolRunCreationFailed)
	}
	if StateReasonCodeProtocolRunFailed != "protocol_run_failed" {
		t.Errorf("protocol run failure code = %q", StateReasonCodeProtocolRunFailed)
	}
	if StateReasonCodeProtocolContractViolated != "protocol_contract_violated" {
		t.Errorf("protocol contract violation code = %q", StateReasonCodeProtocolContractViolated)
	}
}

func TestSnapshotCloneDoesNotAliasOptionalValues(t *testing.T) {
	establishedAt := time.Unix(100, 0)
	nextRetryAt := time.Unix(300, 0)
	original := Snapshot{
		StateReason:                 &StateReason{Code: "original"},
		AuthenticationEstablishedAt: &establishedAt,
		NextRetryAt:                 &nextRetryAt,
		SelectedNetworkBinding: &NetworkBindingSummary{
			InterfaceID:      "ethernet-1",
			LocalIPv4Address: netip.MustParseAddr("192.0.2.10"),
		},
		LastAuthenticationFailure: &AuthenticationFailure{Code: "original"},
	}

	cloned := original.Clone()
	cloned.StateReason.Code = "changed"
	*cloned.AuthenticationEstablishedAt = time.Unix(200, 0)
	*cloned.NextRetryAt = time.Unix(400, 0)
	cloned.SelectedNetworkBinding.InterfaceID = "changed"
	cloned.LastAuthenticationFailure.Code = "changed"

	if original.StateReason.Code != "original" {
		t.Fatal("state reason is aliased")
	}
	if original.AuthenticationEstablishedAt.Equal(*cloned.AuthenticationEstablishedAt) {
		t.Fatal("authentication time is aliased")
	}
	if original.NextRetryAt.Equal(*cloned.NextRetryAt) {
		t.Fatal("next retry time is aliased")
	}
	if original.SelectedNetworkBinding.InterfaceID != "ethernet-1" {
		t.Fatal("network binding summary is aliased")
	}
	if original.LastAuthenticationFailure.Code != "original" {
		t.Fatal("authentication failure is aliased")
	}
}

func TestSnapshotDoesNotContainCredentialOrDiagnosticCause(t *testing.T) {
	snapshotType := reflect.TypeFor[Snapshot]()
	forbidden := map[string]bool{
		"AuthenticationCredential": true,
		"Password":                 true,
		"DiagnosticCause":          true,
		"Generation":               true,
	}
	for index := 0; index < snapshotType.NumField(); index++ {
		if forbidden[snapshotType.Field(index).Name] {
			t.Fatalf("Snapshot exposes forbidden field %q", snapshotType.Field(index).Name)
		}
	}
}

func TestSnapshotDoesNotExposeOneShotCredential(t *testing.T) {
	definition := validRuntimeDefinition(t)
	definition.Configuration.CredentialID = ""
	definition.AuthenticationCredential = credential.AuthenticationCredential{
		Username: "oneshot-account",
		Password: "oneshot-password-secret",
	}

	authSession, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time {
		return time.Unix(100, 0)
	}))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	authSession.Start()
	defer func() { _ = authSession.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshot, err := authSession.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot) error = %v", err)
	}
	public := string(encoded)
	for _, secret := range []string{
		definition.AuthenticationCredential.Username,
		definition.AuthenticationCredential.Password,
	} {
		if strings.Contains(public, secret) {
			t.Fatalf("public Snapshot exposes credential value %q: %s", secret, public)
		}
	}
}

func TestRuntimeDefinitionCloneDoesNotAliasOpaqueProtocolJSON(t *testing.T) {
	original := validRuntimeDefinition(t)
	cloned := original.Clone()
	cloned.Configuration.ProtocolContextOverride[0] ^= 0xff
	cloned.InstitutionProfile.InstitutionProtocolConfiguration[0] ^= 0xff

	if bytes.Equal(
		original.Configuration.ProtocolContextOverride,
		cloned.Configuration.ProtocolContextOverride,
	) {
		t.Fatal("protocol override is aliased")
	}
	if bytes.Equal(
		original.InstitutionProfile.InstitutionProtocolConfiguration,
		cloned.InstitutionProfile.InstitutionProtocolConfiguration,
	) {
		t.Fatal("institution configuration is aliased")
	}
}

func validRuntimeDefinition(t *testing.T) RuntimeDefinition {
	t.Helper()

	return RuntimeDefinition{
		Configuration: Configuration{
			AuthenticationSessionID: "session-1",
			DisplayName:             "Campus network",
			InstitutionProfileID:    "profile-1",
			CredentialID:            "credential-1",
			NetworkBindingPolicy: NetworkBindingPolicy{
				Mode: AutomaticallySelectLatestAvailable,
			},
			ProtocolContextOverride: []byte(`{"network":"campus"}`),
		},
		AuthenticationCredential: credential.AuthenticationCredential{
			Username: "test-account",
			Password: "secret-password",
		},
		InstitutionProfile: profile.InstitutionProfile{
			InstitutionProfileID:             "profile-1",
			DisplayName:                      "Campus",
			AuthenticationProtocolID:         "test-protocol",
			InstitutionProtocolConfiguration: []byte(`{"realm":"campus"}`),
		},
		AuthenticationProtocolFactory: stubFactory{protocolID: "test-protocol"},
		SystemHostInformation: environment.SystemHostInformation{
			HostName:              "test-host",
			OperatingSystemFamily: "test-os",
			MachineArchitecture:   "test-architecture",
		},
	}
}

type stubFactory struct {
	protocolID                    protocol.AuthenticationProtocolID
	institutionConfigurationError error
	protocolContextOverrideError  error
}

func (factory stubFactory) ProtocolID() protocol.AuthenticationProtocolID {
	return factory.protocolID
}

func (factory stubFactory) ValidateInstitutionProtocolConfiguration(
	configuration protocol.InstitutionProtocolConfiguration,
) error {
	if factory.institutionConfigurationError != nil {
		return factory.institutionConfigurationError
	}
	return validateJSON(configuration)
}

func (factory stubFactory) ValidateProtocolContextOverride(
	override protocol.AuthenticationProtocolContextOverride,
) error {
	if factory.protocolContextOverrideError != nil {
		return factory.protocolContextOverrideError
	}
	return validateJSON(override)
}

func (factory stubFactory) CreateAuthenticationProtocolRun(
	protocol.AuthenticationProtocolRunCreationInputs,
) (protocol.AuthenticationProtocolRun, error) {
	return stubRun{}, nil
}

func validateJSON(value []byte) error {
	if json.Valid(value) {
		return nil
	}
	return errors.New("invalid JSON")
}

type stubRun struct{}

func (stubRun) Execute(context.Context, protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	return nil
}

var _ protocol.AuthenticationProtocolFactory = stubFactory{}
var _ protocol.AuthenticationProtocolRun = stubRun{}
