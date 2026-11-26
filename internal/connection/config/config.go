package config

import (
	"errors"

	"sidravia/internal/connection/binding"
	"sidravia/internal/connection/profile"
)

type ID string

type Configuration struct {
	ConfigurationID         ID
	DisplayName             string
	InstitutionProfileID    profile.ID
	Username                string
	NetworkBindingPolicy    binding.Policy
	ProtocolContextOverride profile.ProtocolOverride
	AutoLogin               bool
	AutoReconnect           bool
}

func (configuration Configuration) Clone() Configuration {
	configuration.ProtocolContextOverride = append(
		profile.ProtocolOverride(nil),
		configuration.ProtocolContextOverride...,
	)
	return configuration
}

func (configuration Configuration) Validate() error {
	switch {
	case !configuration.ConfigurationID.Valid():
		return errors.New("configuration id is required")
	case configuration.InstitutionProfileID == "":
		return errors.New("institution profile id is required")
	case configuration.Username == "":
		return errors.New("username is required")
	case configuration.NetworkBindingPolicy.Validate() != nil:
		return errors.New("invalid network binding policy")
	default:
		return nil
	}
}

func (id ID) Valid() bool {
	value := string(id)
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
