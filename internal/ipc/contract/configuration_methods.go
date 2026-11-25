package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	MethodConfigurationList        = "configuration.list"
	MethodConfigurationGet         = "configuration.get"
	MethodConfigurationCreate      = "configuration.create"
	MethodConfigurationUpdate      = "configuration.update"
	MethodConfigurationSetPassword = "configuration.setPassword"
	MethodConfigurationRemove      = "configuration.remove"
)

type ConfigurationIDPayload struct {
	ConfigurationID string `json:"configurationId"`
}
type ConfigurationCreatePayload struct {
	OverrideDocument        *string              `json:"overrideDocument,omitempty"`
	ProtocolContextOverride json.RawMessage      `json:"protocolContextOverride,omitempty"`
	NetworkBindingPolicy    NetworkBindingPolicy `json:"networkBindingPolicy"`
	ConfigurationID         string               `json:"configurationId,omitempty"`
	DisplayName             string               `json:"displayName,omitempty"`
	InstitutionProfileID    string               `json:"institutionProfileId"`
	Username                string               `json:"username"`
	Password                string               `json:"password"`
	AllowInsecureStorage    bool                 `json:"allowInsecureStorage"`
	AutoLogin               bool                 `json:"autoLogin"`
	AutoReconnect           bool                 `json:"autoReconnect"`
}
type ConfigurationUpdatePayload struct {
	OverrideDocument        *string               `json:"overrideDocument,omitempty"`
	ExpectedVersion         string                `json:"expectedVersion,omitempty"`
	ProtocolContextOverride json.RawMessage       `json:"protocolContextOverride,omitempty"`
	NetworkBindingPolicy    *NetworkBindingPolicy `json:"networkBindingPolicy,omitempty"`
	Password                *string               `json:"password,omitempty"`
	AllowInsecureStorage    bool                  `json:"allowInsecureStorage,omitempty"`
	ConfigurationID         string                `json:"configurationId"`
	DisplayName             *string               `json:"displayName,omitempty"`
	InstitutionProfileID    *string               `json:"institutionProfileId,omitempty"`
	Username                *string               `json:"username,omitempty"`
	AutoLogin               *bool                 `json:"autoLogin,omitempty"`
	AutoReconnect           *bool                 `json:"autoReconnect,omitempty"`
}
type ConfigurationSetPasswordPayload struct {
	ConfigurationID      string `json:"configurationId"`
	Password             string `json:"password"`
	AllowInsecureStorage bool   `json:"allowInsecureStorage"`
}
type ConfigurationResult struct {
	Version                  string               `json:"version,omitempty"`
	OverrideState            string               `json:"overrideState,omitempty"`
	OverrideSchemaVersion    int                  `json:"overrideSchemaVersion,omitempty"`
	RuntimeAvailability      string               `json:"runtimeAvailability"`
	ConfigurationID          string               `json:"configurationId"`
	DisplayName              string               `json:"displayName"`
	InstitutionProfileID     string               `json:"institutionProfileId"`
	InstitutionDisplayName   string               `json:"institutionDisplayName"`
	AuthenticationProtocolID string               `json:"authenticationProtocolId"`
	Username                 string               `json:"username"`
	CredentialStored         bool                 `json:"credentialStored"`
	StorageProtection        string               `json:"storageProtection"`
	AutoLogin                bool                 `json:"autoLogin"`
	AutoReconnect            bool                 `json:"autoReconnect"`
	NetworkBindingPolicy     NetworkBindingPolicy `json:"networkBindingPolicy"`
}
type ConfigurationListResult struct {
	StorageProtection string                `json:"storageProtection"`
	Configurations    []ConfigurationResult `json:"configurations"`
}
type ConfigurationRemoveResult struct {
	ConfigurationID string `json:"configurationId"`
	Status          string `json:"status"`
}

func DecodeConfigurationIDPayload(data []byte) (ConfigurationIDPayload, error) {
	var value ConfigurationIDPayload
	if err := decodeRequestPayload(data, &value, "configurationId"); err != nil {
		return ConfigurationIDPayload{}, err
	}
	if value.ConfigurationID == "" {
		return ConfigurationIDPayload{}, fmt.Errorf("missing configurationId")
	}
	return value, nil
}
func DecodeConfigurationCreatePayload(data []byte) (ConfigurationCreatePayload, error) {
	var wire struct {
		OverrideDocument        *string               `json:"overrideDocument"`
		ProtocolContextOverride json.RawMessage       `json:"protocolContextOverride"`
		NetworkBindingPolicy    *NetworkBindingPolicy `json:"networkBindingPolicy"`
		ConfigurationID         *string               `json:"configurationId"`
		DisplayName             *string               `json:"displayName"`
		InstitutionProfileID    *string               `json:"institutionProfileId"`
		Username                *string               `json:"username"`
		Password                *string               `json:"password"`
		AllowInsecureStorage    *bool                 `json:"allowInsecureStorage"`
		AutoLogin               *bool                 `json:"autoLogin"`
		AutoReconnect           *bool                 `json:"autoReconnect"`
	}
	if err := decodeRequestPayload(data, &wire, "overrideDocument", "protocolContextOverride", "networkBindingPolicy", "configurationId", "displayName", "institutionProfileId", "username", "password", "allowInsecureStorage", "autoLogin", "autoReconnect"); err != nil {
		return ConfigurationCreatePayload{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ConfigurationCreatePayload{}, err
	}
	for _, name := range []string{"configurationId", "displayName"} {
		if raw, present := fields[name]; present && string(raw) == "null" {
			return ConfigurationCreatePayload{}, fmt.Errorf("configuration create %s must be a string", name)
		}
	}
	if wire.NetworkBindingPolicy == nil || wire.InstitutionProfileID == nil ||
		wire.Username == nil || wire.Password == nil || wire.AllowInsecureStorage == nil || wire.AutoLogin == nil || wire.AutoReconnect == nil ||
		wire.ConfigurationID != nil && *wire.ConfigurationID == "" || *wire.InstitutionProfileID == "" || *wire.Username == "" {
		return ConfigurationCreatePayload{}, fmt.Errorf("missing required configuration field")
	}
	var override json.RawMessage
	if err := readOverrideDocument(data, wire.OverrideDocument, &wire.ProtocolContextOverride); err != nil {
		return ConfigurationCreatePayload{}, err
	}
	if len(wire.ProtocolContextOverride) > 0 {
		var err error
		override, err = DecodeProtocolContextOverride(wire.ProtocolContextOverride)
		if err != nil {
			return ConfigurationCreatePayload{}, err
		}
	}
	value := ConfigurationCreatePayload{
		ProtocolContextOverride: override,
		NetworkBindingPolicy:    *wire.NetworkBindingPolicy,
		InstitutionProfileID:    *wire.InstitutionProfileID, Username: *wire.Username,
		Password: *wire.Password, AllowInsecureStorage: *wire.AllowInsecureStorage,
		AutoLogin: *wire.AutoLogin, AutoReconnect: *wire.AutoReconnect,
	}
	if wire.ConfigurationID != nil {
		value.ConfigurationID = *wire.ConfigurationID
	}
	if wire.DisplayName != nil {
		value.DisplayName = *wire.DisplayName
	}
	return value, nil
}

type ConfigurationRemovePayload struct {
	ConfigurationID      string `json:"configurationId"`
	ExpectedVersion      string `json:"expectedVersion"`
	AllowInsecureStorage bool   `json:"allowInsecureStorage,omitempty"`
}

func DecodeConfigurationRemovePayload(data []byte) (ConfigurationRemovePayload, error) {
	var value ConfigurationRemovePayload
	if err := decodeRequestPayload(data, &value, "configurationId", "expectedVersion", "allowInsecureStorage"); err != nil {
		return value, err
	}
	if value.ConfigurationID == "" || value.ExpectedVersion == "" {
		return value, fmt.Errorf("missing configurationId or expectedVersion")
	}
	if err := rejectNullFields(data, "allowInsecureStorage"); err != nil {
		return value, err
	}
	return value, nil
}
func rejectNullFields(data []byte, names ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range names {
		if raw, ok := fields[name]; ok && string(raw) == "null" {
			return fmt.Errorf("null optional configuration field")
		}
	}
	return nil
}
func DecodeConfigurationUpdatePayload(data []byte) (ConfigurationUpdatePayload, error) {
	var value ConfigurationUpdatePayload
	if err := decodeRequestPayload(data, &value, "expectedVersion", "overrideDocument", "protocolContextOverride", "networkBindingPolicy", "password", "allowInsecureStorage", "configurationId", "displayName", "institutionProfileId", "username", "autoLogin", "autoReconnect"); err != nil {
		return ConfigurationUpdatePayload{}, err
	}
	if err := rejectNullFields(data, "expectedVersion", "displayName", "institutionProfileId", "username", "password", "autoLogin", "autoReconnect", "allowInsecureStorage", "networkBindingPolicy"); err != nil {
		return ConfigurationUpdatePayload{}, err
	}
	if err := readOverrideDocument(data, value.OverrideDocument, &value.ProtocolContextOverride); err != nil {
		return ConfigurationUpdatePayload{}, err
	}
	if value.ConfigurationID == "" || value.DisplayName == nil && value.InstitutionProfileID == nil && value.Username == nil && value.AutoLogin == nil && value.AutoReconnect == nil && value.Password == nil && value.NetworkBindingPolicy == nil && len(value.ProtocolContextOverride) == 0 {
		return ConfigurationUpdatePayload{}, fmt.Errorf("missing configuration update")
	}
	if value.InstitutionProfileID != nil && *value.InstitutionProfileID == "" || value.Username != nil && *value.Username == "" {
		return ConfigurationUpdatePayload{}, fmt.Errorf("empty required configuration field")
	}
	if len(value.ProtocolContextOverride) > 0 && !bytes.Equal(bytes.TrimSpace(value.ProtocolContextOverride), []byte("null")) {
		raw, err := DecodeProtocolContextOverride(value.ProtocolContextOverride)
		if err != nil {
			return ConfigurationUpdatePayload{}, err
		}
		value.ProtocolContextOverride = raw
	}
	return value, nil
}
func DecodeConfigurationSetPasswordPayload(data []byte) (ConfigurationSetPasswordPayload, error) {
	var wire struct {
		ConfigurationID      *string `json:"configurationId"`
		Password             *string `json:"password"`
		AllowInsecureStorage *bool   `json:"allowInsecureStorage"`
	}
	if err := decodeRequestPayload(data, &wire, "configurationId", "password", "allowInsecureStorage"); err != nil {
		return ConfigurationSetPasswordPayload{}, err
	}
	if wire.ConfigurationID == nil || wire.Password == nil || wire.AllowInsecureStorage == nil ||
		*wire.ConfigurationID == "" {
		return ConfigurationSetPasswordPayload{}, fmt.Errorf("missing configurationId")
	}
	return ConfigurationSetPasswordPayload{
		ConfigurationID: *wire.ConfigurationID, Password: *wire.Password,
		AllowInsecureStorage: *wire.AllowInsecureStorage,
	}, nil
}
func MarshalConfigurationRemoveResult(value ConfigurationRemoveResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}

// DecodeConfigurationRemoveResult verifies that the receipt identifies the requested configuration.
func DecodeConfigurationRemoveResult(data []byte, expectedConfigurationID string) (ConfigurationRemoveResult, error) {
	var result ConfigurationRemoveResult
	if err := networkObject(data, []string{"configurationId", "status"}); err != nil {
		return result, err
	}
	if err := decodeStrict(data, &result); err != nil {
		return ConfigurationRemoveResult{}, err
	}
	if result.ConfigurationID == "" || result.ConfigurationID != expectedConfigurationID || result.Status != "removed" {
		return ConfigurationRemoveResult{}, fmt.Errorf("invalid configuration removal result")
	}
	return result, nil
}
