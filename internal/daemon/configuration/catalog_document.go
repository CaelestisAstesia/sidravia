package configuration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const catalogSchemaVersion uint64 = 3
const catalogSchemaVersion2 uint64 = 2

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
	ConfigurationID         *ConfigurationID      `json:"configurationId"`
	DisplayName             *string               `json:"displayName"`
	InstitutionProfileID    *InstitutionProfileID `json:"institutionProfileId"`
	Username                *string               `json:"username"`
	Password                *string               `json:"password"`
	NetworkBindingPolicy    json.RawMessage       `json:"networkBindingPolicy"`
	ProtocolContextOverride json.RawMessage       `json:"protocolContextOverride"`
	AutoLogin               *bool                 `json:"autoLogin"`
	AutoReconnect           *bool                 `json:"autoReconnect"`
}
type persistentConfigurationOutput struct {
	ConfigurationID         ConfigurationID                      `json:"configurationId"`
	DisplayName             string                               `json:"displayName"`
	InstitutionProfileID    InstitutionProfileID                 `json:"institutionProfileId"`
	Username                string                               `json:"username"`
	Password                string                               `json:"password"`
	NetworkBindingPolicy    persistentNetworkBindingPolicyOutput `json:"networkBindingPolicy"`
	ProtocolContextOverride json.RawMessage                      `json:"protocolContextOverride"`
	AutoLogin               bool                                 `json:"autoLogin"`
	AutoReconnect           bool                                 `json:"autoReconnect"`
}
type catalogDocumentOutput struct {
	SchemaVersion  uint64                          `json:"schemaVersion"`
	Configurations []persistentConfigurationOutput `json:"configurations"`
}

func decodeCatalogDocument(data []byte) (map[ConfigurationID]catalogRecord, error) {
	var preliminary struct {
		Configurations []json.RawMessage `json:"configurations"`
	}
	if err := json.Unmarshal(data, &preliminary); err != nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
	}
	pointers := make([]string, len(preliminary.Configurations))
	for i := range pointers {
		pointers[i] = fmt.Sprintf("/configurations/%d/protocolContextOverride", i)
	}
	var document catalogDocumentEnvelope
	if err := jsonfile.DecodeStrict(data, &document, pointers...); err != nil {
		return nil, err
	}
	if document.SchemaVersion == nil || document.Configurations == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
	}
	switch *document.SchemaVersion {
	case catalogSchemaVersion2, catalogSchemaVersion:
	default:
		return nil, persistence.NewFailure(persistence.FailureUnsupportedSchemaVersion, nil)
	}
	schemaIsV2 := *document.SchemaVersion == catalogSchemaVersion2
	result := make(map[ConfigurationID]catalogRecord, len(*document.Configurations))
	for _, raw := range *document.Configurations {
		var in persistentConfiguration
		if err := jsonfile.DecodeStrict(raw, &in, "/protocolContextOverride"); err != nil {
			return nil, err
		}
		if in.ConfigurationID == nil || in.DisplayName == nil || in.InstitutionProfileID == nil || in.Username == nil || in.Password == nil || len(in.NetworkBindingPolicy) == 0 || len(in.ProtocolContextOverride) == 0 {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
		}
		var autoLogin, autoReconnect bool
		if schemaIsV2 {
			autoLogin = false
			autoReconnect = true
		} else {
			if in.AutoLogin == nil || in.AutoReconnect == nil {
				return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
			}
			autoLogin = *in.AutoLogin
			autoReconnect = *in.AutoReconnect
		}
		var binding persistentNetworkBindingPolicy
		if err := jsonfile.DecodeStrict(in.NetworkBindingPolicy, &binding); err != nil || binding.Mode == nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
		}
		if err := jsonfile.ValidateOpaqueObjectOrNull(in.ProtocolContextOverride); err != nil {
			return nil, err
		}
		override := protocol.AuthenticationProtocolContextOverride(nil)
		if !bytes.Equal(bytes.TrimSpace(in.ProtocolContextOverride), []byte("null")) {
			override = append(override, in.ProtocolContextOverride...)
		}
		value := Configuration{ConfigurationID: *in.ConfigurationID, DisplayName: *in.DisplayName, InstitutionProfileID: *in.InstitutionProfileID, Username: *in.Username, NetworkBindingPolicy: NetworkBindingPolicy{Mode: *binding.Mode}, ProtocolContextOverride: override, AutoLogin: autoLogin, AutoReconnect: autoReconnect}
		if err := value.Validate(); err != nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
		}
		if _, exists := result[value.ConfigurationID]; exists {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
		}
		if autoLogin {
			for _, existing := range result {
				if existing.configuration.AutoLogin {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
			}
		}
		result[value.ConfigurationID] = catalogRecord{configuration: value, password: *in.Password}
	}
	return result, nil
}

func encodeCatalogDocument(records map[ConfigurationID]catalogRecord) ([]byte, error) {
	ids := make([]ConfigurationID, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]persistentConfigurationOutput, 0, len(ids))
	for _, id := range ids {
		record := records[id]
		if record.configuration.ConfigurationID != id || record.configuration.Validate() != nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidArgument, nil)
		}
		override := json.RawMessage("null")
		if len(record.configuration.ProtocolContextOverride) > 0 {
			override = append(json.RawMessage(nil), record.configuration.ProtocolContextOverride...)
		}
		out = append(out, persistentConfigurationOutput{ConfigurationID: id, DisplayName: record.configuration.DisplayName, InstitutionProfileID: record.configuration.InstitutionProfileID, Username: record.configuration.Username, Password: record.password, NetworkBindingPolicy: persistentNetworkBindingPolicyOutput{Mode: record.configuration.NetworkBindingPolicy.Mode}, ProtocolContextOverride: override, AutoLogin: record.configuration.AutoLogin, AutoReconnect: record.configuration.AutoReconnect})
	}
	return jsonfile.MarshalDeterministic(catalogDocumentOutput{SchemaVersion: catalogSchemaVersion, Configurations: out})
}
