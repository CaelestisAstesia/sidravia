package contract

import (
	"encoding/json"
	"errors"
)

func (value *ConfigurationResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"configurationId", "displayName", "institutionProfileId", "institutionDisplayName", "authenticationProtocolId", "username", "credentialStored", "storageProtection", "autoLogin", "autoReconnect", "networkBindingPolicy", "runtimeAvailability"}); err != nil {
		return err
	}
	type plain ConfigurationResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.ConfigurationID == "" || wire.InstitutionProfileID == "" || wire.Username == "" || !resultProtection(wire.StorageProtection) {
		return errors.New("invalid configuration result")
	}
	if _, err := wire.NetworkBindingPolicy.Domain(); err != nil {
		return err
	}
	switch wire.RuntimeAvailability {
	case "profile_unavailable":
		if wire.InstitutionDisplayName != "" || wire.AuthenticationProtocolID != "" {
			return errors.New("unexpected unavailable Profile metadata")
		}
	case "available", "protocol_unavailable", "override_invalid":
		if wire.InstitutionDisplayName == "" || wire.AuthenticationProtocolID == "" {
			return errors.New("missing known Profile metadata")
		}
	default:
		return errors.New("invalid runtime availability")
	}
	*value = ConfigurationResult(wire)
	return nil
}
func resultProtection(value string) bool { return value == "protected" || value == "unprotected" }
func (value *ConfigurationListResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"storageProtection", "configurations"}); err != nil {
		return err
	}
	type plain ConfigurationListResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if !resultProtection(wire.StorageProtection) || wire.Configurations == nil {
		return errors.New("invalid configuration list")
	}
	*value = ConfigurationListResult(wire)
	return nil
}
func DecodeConfigurationResult(data []byte) (ConfigurationResult, error) {
	var value ConfigurationResult
	err := decodeStrict(data, &value)
	return value, err
}
func DecodeConfigurationListResult(data []byte) (ConfigurationListResult, error) {
	var value ConfigurationListResult
	err := decodeStrict(data, &value)
	return value, err
}
func MarshalConfigurationResult(value ConfigurationResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = DecodeConfigurationResult(data)
	return data, err
}
func MarshalConfigurationListResult(value ConfigurationListResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = DecodeConfigurationListResult(data)
	return data, err
}
