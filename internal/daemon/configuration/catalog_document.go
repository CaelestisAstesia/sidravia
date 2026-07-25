package configuration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type catalogDocumentEnvelope struct {
	SchemaVersion  *uint64            `json:"schemaVersion"`
	Configurations *[]json.RawMessage `json:"configurations"`
}

type persistentNetworkBindingPolicy struct {
	Mode *NetworkBindingPolicyMode `json:"mode"`
}

type persistentNetworkBindingPolicyOutput struct {
	Mode NetworkBindingPolicyMode `json:"mode"`
}

type persistentConfiguration struct {
	ConfigurationID         *ConfigurationID          `json:"configurationId"`
	DisplayName             *string                   `json:"displayName"`
	InstitutionProfileID    *InstitutionProfileID     `json:"institutionProfileId"`
	CredentialID            *credentials.CredentialID `json:"credentialId"`
	NetworkBindingPolicy    json.RawMessage           `json:"networkBindingPolicy"`
	ProtocolContextOverride json.RawMessage           `json:"protocolContextOverride"`
}

type persistentConfigurationOutput struct {
	ConfigurationID         ConfigurationID                      `json:"configurationId"`
	DisplayName             string                               `json:"displayName"`
	InstitutionProfileID    InstitutionProfileID                 `json:"institutionProfileId"`
	CredentialID            credentials.CredentialID             `json:"credentialId"`
	NetworkBindingPolicy    persistentNetworkBindingPolicyOutput `json:"networkBindingPolicy"`
	ProtocolContextOverride json.RawMessage                      `json:"protocolContextOverride"`
}

type catalogDocumentOutput struct {
	SchemaVersion  uint64                          `json:"schemaVersion"`
	Configurations []persistentConfigurationOutput `json:"configurations"`
}

func decodeCatalogDocument(data []byte) (map[ConfigurationID]Configuration, error) {
	var preliminary struct {
		Configurations []json.RawMessage `json:"configurations"`
	}
	if err := json.Unmarshal(data, &preliminary); err != nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	opaquePointers := make([]string, len(preliminary.Configurations))
	for index := range preliminary.Configurations {
		opaquePointers[index] = fmt.Sprintf("/configurations/%d/protocolContextOverride", index)
	}

	var document catalogDocumentEnvelope
	if err := jsonfile.DecodeStrict(data, &document, opaquePointers...); err != nil {
		return nil, err
	}
	if document.SchemaVersion == nil || document.Configurations == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("required catalog field missing"))
	}
	if *document.SchemaVersion != jsonfile.SchemaVersion1 {
		return nil, persistence.NewFailure(persistence.FailureUnsupportedSchemaVersion, nil)
	}
	configurations := make(map[ConfigurationID]Configuration, len(*document.Configurations))
	for _, raw := range *document.Configurations {
		configuration, err := decodePersistentConfiguration(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := configurations[configuration.ConfigurationID]; exists {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("duplicate configuration id"))
		}
		configurations[configuration.ConfigurationID] = configuration
	}
	return configurations, nil
}

func decodePersistentConfiguration(data []byte) (Configuration, error) {
	var record persistentConfiguration
	if err := jsonfile.DecodeStrict(data, &record, "/protocolContextOverride"); err != nil {
		return Configuration{}, err
	}
	if record.ConfigurationID == nil || record.DisplayName == nil || record.InstitutionProfileID == nil || record.CredentialID == nil || len(record.NetworkBindingPolicy) == 0 || len(record.ProtocolContextOverride) == 0 {
		return Configuration{}, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("required configuration field missing"))
	}
	if trimmed := bytes.TrimSpace(record.NetworkBindingPolicy); len(trimmed) == 0 || trimmed[0] != '{' {
		return Configuration{}, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("network binding policy is not an object"))
	}
	var binding persistentNetworkBindingPolicy
	if err := jsonfile.DecodeStrict(record.NetworkBindingPolicy, &binding); err != nil {
		return Configuration{}, err
	}
	if binding.Mode == nil {
		return Configuration{}, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("network binding policy mode missing"))
	}
	if err := jsonfile.ValidateOpaqueObjectOrNull(record.ProtocolContextOverride); err != nil {
		return Configuration{}, err
	}

	override := protocol.AuthenticationProtocolContextOverride(nil)
	if !bytes.Equal(bytes.TrimSpace(record.ProtocolContextOverride), []byte("null")) {
		override = append(override, record.ProtocolContextOverride...)
	}
	configuration := Configuration{
		ConfigurationID:         *record.ConfigurationID,
		DisplayName:             *record.DisplayName,
		InstitutionProfileID:    *record.InstitutionProfileID,
		CredentialID:            *record.CredentialID,
		NetworkBindingPolicy:    NetworkBindingPolicy{Mode: *binding.Mode},
		ProtocolContextOverride: override,
	}
	if err := configuration.Validate(); err != nil {
		return Configuration{}, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	return configuration, nil
}

func encodeCatalogDocument(configurations map[ConfigurationID]Configuration) ([]byte, error) {
	identifiers := make([]ConfigurationID, 0, len(configurations))
	for identifier := range configurations {
		identifiers = append(identifiers, identifier)
	}
	sort.Slice(identifiers, func(left, right int) bool { return identifiers[left] < identifiers[right] })

	records := make([]persistentConfigurationOutput, 0, len(identifiers))
	for _, identifier := range identifiers {
		configuration := configurations[identifier].Clone()
		if configuration.ConfigurationID != identifier {
			return nil, persistence.NewFailure(persistence.FailureInvalidArgument, fmt.Errorf("configuration id does not match map key"))
		}
		if err := configuration.Validate(); err != nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidArgument, err)
		}
		override := json.RawMessage("null")
		if len(configuration.ProtocolContextOverride) > 0 {
			override = append(json.RawMessage(nil), configuration.ProtocolContextOverride...)
			if err := jsonfile.ValidateOpaqueObjectOrNull(override); err != nil {
				return nil, persistence.NewFailure(persistence.FailureInvalidArgument, err)
			}
		}
		records = append(records, persistentConfigurationOutput{
			ConfigurationID:         configuration.ConfigurationID,
			DisplayName:             configuration.DisplayName,
			InstitutionProfileID:    configuration.InstitutionProfileID,
			CredentialID:            configuration.CredentialID,
			NetworkBindingPolicy:    persistentNetworkBindingPolicyOutput{Mode: configuration.NetworkBindingPolicy.Mode},
			ProtocolContextOverride: override,
		})
	}
	return jsonfile.MarshalDeterministic(catalogDocumentOutput{
		SchemaVersion:  jsonfile.SchemaVersion1,
		Configurations: records,
	})
}
