package configuration

import (
	"errors"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/credentials"
)

type ConfigurationID string

type NetworkBindingPolicyMode string

const AutomaticallySelectLatestAvailable NetworkBindingPolicyMode = "automatically_select_latest_available"

type NetworkBindingPolicy struct {
	Mode NetworkBindingPolicyMode
}

type Configuration struct {
	ConfigurationID         ConfigurationID
	DisplayName             string
	InstitutionProfileID    InstitutionProfileID
	CredentialID            credentials.CredentialID
	NetworkBindingPolicy    NetworkBindingPolicy
	ProtocolContextOverride protocol.AuthenticationProtocolContextOverride
}

func (configuration Configuration) Clone() Configuration {
	configuration.ProtocolContextOverride = append(
		protocol.AuthenticationProtocolContextOverride(nil),
		configuration.ProtocolContextOverride...,
	)
	return configuration
}

func (configuration Configuration) Validate() error {
	switch {
	case configuration.ConfigurationID == "":
		return errors.New("configuration id is required")
	case configuration.InstitutionProfileID == "":
		return errors.New("institution profile id is required")
	case configuration.CredentialID == "":
		return errors.New("credential id is required")
	case configuration.NetworkBindingPolicy.Mode != AutomaticallySelectLatestAvailable:
		return errors.New("unsupported network binding policy")
	default:
		return nil
	}
}
