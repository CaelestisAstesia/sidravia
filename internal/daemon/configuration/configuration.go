package configuration

import (
	"errors"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"sidravia/internal/daemon/authentication/protocol"
)

type ConfigurationID string

type NetworkBindingPolicyMode string

const AutomaticallySelectLatestAvailable NetworkBindingPolicyMode = "automatically_select_latest_available"
const ExplicitInterfaceAndLocalIPv4 NetworkBindingPolicyMode = "explicit_interface_and_local_ipv4"

type NetworkBindingPolicy struct {
	Mode             NetworkBindingPolicyMode
	InterfaceID      string
	LocalIPv4Address netip.Addr
}

func (policy NetworkBindingPolicy) Validate() error {
	switch policy.Mode {
	case AutomaticallySelectLatestAvailable:
		if policy.InterfaceID != "" || policy.LocalIPv4Address.IsValid() {
			return errors.New("automatic binding must have no target")
		}
	case ExplicitInterfaceAndLocalIPv4:
		id := policy.InterfaceID
		if len(id) < 1 || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
			return errors.New("invalid interface id")
		}
		for _, r := range id {
			if unicode.IsControl(r) {
				return errors.New("invalid interface id")
			}
		}
		address := policy.LocalIPv4Address
		if !address.Is4() || address.IsUnspecified() || address.IsMulticast() {
			return errors.New("invalid local IPv4 address")
		}
	default:
		return errors.New("unsupported network binding policy")
	}
	return nil
}

type Configuration struct {
	ConfigurationID         ConfigurationID
	DisplayName             string
	InstitutionProfileID    InstitutionProfileID
	Username                string
	NetworkBindingPolicy    NetworkBindingPolicy
	ProtocolContextOverride protocol.AuthenticationProtocolContextOverride
	AutoLogin               bool
	AutoReconnect           bool
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
	case configuration.NetworkBindingPolicy.Validate() != nil:
		return errors.New("invalid network binding policy")
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
