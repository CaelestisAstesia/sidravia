package session

import (
	"errors"
	"fmt"

	"sidravia/internal/daemon/authentication/protocol"
	profile "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	environment "sidravia/internal/daemon/environment"
)

type RuntimeDefinition struct {
	Configuration                 Configuration
	AuthenticationCredential      credential.AuthenticationCredential
	InstitutionProfile            profile.InstitutionProfile
	AuthenticationProtocolFactory protocol.AuthenticationProtocolFactory
	SystemHostInformation         environment.SystemHostInformation
}

func (definition RuntimeDefinition) Validate() error {
	if definition.Configuration.AuthenticationSessionID == "" {
		return fmt.Errorf("authentication session ID is required")
	}
	if definition.Configuration.InstitutionProfileID == "" {
		return fmt.Errorf("institution profile ID is required")
	}
	// CredentialID is intentionally not required here. A resolved one-shot
	// definition supplies its credential directly and has no persistent
	// CredentialID; persistent definitions always carry one because the
	// configuration catalog rejects an empty CredentialID on save.
	if definition.Configuration.NetworkBindingPolicy.Mode != AutomaticallySelectLatestAvailable {
		return fmt.Errorf("unsupported network binding policy mode %q", definition.Configuration.NetworkBindingPolicy.Mode)
	}
	if definition.InstitutionProfile.InstitutionProfileID == "" {
		return fmt.Errorf("institution profile ID is required")
	}
	if definition.InstitutionProfile.InstitutionProfileID != definition.Configuration.InstitutionProfileID {
		return fmt.Errorf("institution profile ID does not match configuration")
	}
	if definition.InstitutionProfile.AuthenticationProtocolID == "" {
		return fmt.Errorf("authentication protocol ID is required")
	}
	if definition.AuthenticationProtocolFactory == nil {
		return fmt.Errorf("authentication protocol factory is required")
	}
	if definition.AuthenticationProtocolFactory.ProtocolID() == "" {
		return fmt.Errorf("authentication protocol factory ID is required")
	}
	if definition.InstitutionProfile.AuthenticationProtocolID != definition.AuthenticationProtocolFactory.ProtocolID() {
		return fmt.Errorf("authentication protocol ID does not match factory")
	}
	if err := definition.AuthenticationProtocolFactory.ValidateInstitutionProtocolConfiguration(
		definition.InstitutionProfile.InstitutionProtocolConfiguration,
	); err != nil {
		return errors.New("institution protocol configuration is invalid")
	}
	if err := definition.AuthenticationProtocolFactory.ValidateProtocolContextOverride(
		definition.Configuration.ProtocolContextOverride,
	); err != nil {
		return errors.New("protocol context override is invalid")
	}
	return nil
}

// AccountName returns the complete authentication username. CLI and daemon
// share one build/version, so the public Snapshot carries the full account
// name instead of a masked label; Password and CredentialID remain absent.
// Control-character sanitization happens at the CLI presentation boundary.
func (definition RuntimeDefinition) AccountName() string {
	return definition.AuthenticationCredential.Username
}

func (definition RuntimeDefinition) Clone() RuntimeDefinition {
	cloned := definition
	cloned.Configuration.ProtocolContextOverride = append(
		protocol.AuthenticationProtocolContextOverride(nil),
		definition.Configuration.ProtocolContextOverride...,
	)
	cloned.InstitutionProfile.InstitutionProtocolConfiguration = append(
		protocol.InstitutionProtocolConfiguration(nil),
		definition.InstitutionProfile.InstitutionProtocolConfiguration...,
	)
	return cloned
}

func cloneProtocolContextOverride(
	override protocol.AuthenticationProtocolContextOverride,
) protocol.AuthenticationProtocolContextOverride {
	if override == nil {
		return nil
	}
	cloned := make(protocol.AuthenticationProtocolContextOverride, len(override))
	copy(cloned, override)
	return cloned
}
