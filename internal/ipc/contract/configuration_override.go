package contract

import (
	"encoding/json"
	"errors"
)

const (
	MethodConfigurationGetOverride        = "configuration.getOverride"
	MethodConfigurationValidateOverride   = "configuration.validateOverride"
	ErrorCodeConfigurationVersionConflict = "configuration_version_conflict"
)

type ConfigurationOverrideResult struct {
	Configuration   ConfigurationResult `json:"configuration"`
	Document        *string             `json:"document"`
	MatchesDocument *bool               `json:"matchesDocument,omitempty"`
}

type ConfigurationReadOverridePayload struct {
	ConfigurationID  string  `json:"configurationId"`
	ExpectedDocument *string `json:"expectedDocument,omitempty"`
}

func DecodeConfigurationReadOverridePayload(data []byte) (ConfigurationReadOverridePayload, error) {
	var value ConfigurationReadOverridePayload
	if err := decodeRequestPayload(data, &value, "configurationId", "expectedDocument"); err != nil {
		return value, err
	}
	if value.ConfigurationID == "" {
		return value, errors.New("missing configuration ID")
	}
	if err := rejectNullFields(data, "expectedDocument"); err != nil {
		return value, err
	}
	if value.ExpectedDocument != nil {
		if _, err := DecodeProtocolContextOverride([]byte(*value.ExpectedDocument)); err != nil {
			return value, err
		}
	}
	return value, nil
}

type ConfigurationValidateOverridePayload struct {
	InstitutionProfileID string `json:"institutionProfileId"`
	Document             string `json:"document"`
}

func DecodeConfigurationValidateOverridePayload(data []byte) (ConfigurationValidateOverridePayload, error) {
	var value ConfigurationValidateOverridePayload
	if err := decodeRequestPayload(data, &value, "institutionProfileId", "document"); err != nil {
		return value, err
	}
	if value.InstitutionProfileID == "" {
		return value, errors.New("missing institution profile")
	}
	return value, nil
}

type ConfigurationStartPayload struct {
	ConfigurationID string `json:"configurationId"`
	ExpectedVersion string `json:"expectedVersion,omitempty"`
}

func DecodeConfigurationStartPayload(data []byte) (ConfigurationStartPayload, error) {
	var value ConfigurationStartPayload
	if err := decodeRequestPayload(data, &value, "configurationId", "expectedVersion"); err != nil {
		return value, err
	}
	if err := rejectNullFields(data, "expectedVersion"); err != nil {
		return value, err
	}
	if value.ConfigurationID == "" {
		return value, errors.New("missing configuration ID")
	}
	return value, nil
}

type ConfigurationOverrideValidationResult struct {
	Valid bool   `json:"valid"`
	Field string `json:"field,omitempty"`
}

// Transporting editor text as a string preserves its bytes for strict validation.
func readOverrideDocument(data []byte, document *string, raw *json.RawMessage) error {
	if err := rejectNullFields(data, "overrideDocument"); err != nil {
		return err
	}
	if document == nil {
		return nil
	}
	if len(*raw) != 0 {
		return errors.New("conflicting override inputs")
	}
	decoded, err := DecodeProtocolContextOverride([]byte(*document))
	if err != nil {
		return err
	}
	*raw = decoded
	return nil
}
