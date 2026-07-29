package configuration

import (
	"errors"

	"sidravia/internal/daemon/authentication/protocol"
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
	Username                string
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
	case !validConfigurationID(string(configuration.ConfigurationID)):
		return errors.New("configuration id is required")
	case configuration.InstitutionProfileID == "":
		return errors.New("institution profile id is required")
	case configuration.Username == "":
		return errors.New("username is required")
	case configuration.NetworkBindingPolicy.Mode != AutomaticallySelectLatestAvailable:
		return errors.New("unsupported network binding policy")
	default:
		return nil
	}
}

func validConfigurationID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for index, character := range []byte(value) {
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || (character == '-' && index > 0 && index < len(value)-1)) {
			return false
		}
	}
	return true
}
