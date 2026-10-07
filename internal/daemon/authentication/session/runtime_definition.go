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
	AutoReconnect                 bool
}

func (definition RuntimeDefinition) Validate() error {
	if definition.Configuration.AuthenticationSessionID == "" {
		return fmt.Errorf("authentication session ID is required")
	}
	if definition.Configuration.InstitutionProfileID == "" {
		return fmt.Errorf("institution profile ID is required")
	}
	if err := definition.Configuration.NetworkBindingPolicy.Validate(); err != nil {
		return err
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
// name instead of a masked label; Password remains absent.
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
