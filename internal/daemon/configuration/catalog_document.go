package configuration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const catalogSchemaVersion uint64 = 4
const catalogSchemaVersion3 uint64 = 3
const catalogSchemaVersion2 uint64 = 2

type catalogDocumentEnvelope struct {
	SchemaVersion  *uint64            `json:"schemaVersion"`
	Configurations *[]json.RawMessage `json:"configurations"`
}
type persistentNetworkBindingPolicy struct {
	Mode             *NetworkBindingPolicyMode `json:"mode"`
	InterfaceID      *string                   `json:"interfaceId"`
	LocalIPv4Address *string                   `json:"localIpv4Address"`
}
type persistentNetworkBindingPolicyOutput struct {
	Mode             NetworkBindingPolicyMode `json:"mode"`
	InterfaceID      string                   `json:"interfaceId,omitempty"`
	LocalIPv4Address string                   `json:"localIpv4Address,omitempty"`
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
	case catalogSchemaVersion2, catalogSchemaVersion3, catalogSchemaVersion:
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
		policy := NetworkBindingPolicy{Mode: *binding.Mode}
		var policyFields map[string]json.RawMessage
		if err := json.Unmarshal(in.NetworkBindingPolicy, &policyFields); err != nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
		}
		if *document.SchemaVersion != catalogSchemaVersion {
			if len(policyFields) != 1 || binding.InterfaceID != nil || binding.LocalIPv4Address != nil || policy.Mode != AutomaticallySelectLatestAvailable {
				return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
			}
		} else {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(in.NetworkBindingPolicy, &fields); err != nil {
				return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
			}
			if policy.Mode == AutomaticallySelectLatestAvailable {
				if len(fields) != 1 {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
			} else {
				if policy.Mode != ExplicitInterfaceAndLocalIPv4 || len(fields) != 3 || binding.InterfaceID == nil || binding.LocalIPv4Address == nil {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
				address, err := netip.ParseAddr(*binding.LocalIPv4Address)
				if err != nil || address.String() != *binding.LocalIPv4Address {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
				policy.InterfaceID = *binding.InterfaceID
				policy.LocalIPv4Address = address
			}
		}
		if err := jsonfile.ValidateOpaqueObjectOrNull(in.ProtocolContextOverride); err != nil {
			return nil, err
		}
		override := protocol.AuthenticationProtocolContextOverride(nil)
		if !bytes.Equal(bytes.TrimSpace(in.ProtocolContextOverride), []byte("null")) {
			override = append(override, in.ProtocolContextOverride...)
		}
		value := Configuration{ConfigurationID: *in.ConfigurationID, DisplayName: *in.DisplayName, InstitutionProfileID: *in.InstitutionProfileID, Username: *in.Username, NetworkBindingPolicy: policy, ProtocolContextOverride: override, AutoLogin: autoLogin, AutoReconnect: autoReconnect}
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
		out = append(out, persistentConfigurationOutput{ConfigurationID: id, DisplayName: record.configuration.DisplayName, InstitutionProfileID: record.configuration.InstitutionProfileID, Username: record.configuration.Username, Password: record.password, NetworkBindingPolicy: persistentPolicyOutput(record.configuration.NetworkBindingPolicy), ProtocolContextOverride: override, AutoLogin: record.configuration.AutoLogin, AutoReconnect: record.configuration.AutoReconnect})
	}
	return jsonfile.MarshalDeterministic(catalogDocumentOutput{SchemaVersion: catalogSchemaVersion, Configurations: out})
}

func persistentPolicyOutput(policy NetworkBindingPolicy) persistentNetworkBindingPolicyOutput {
	out := persistentNetworkBindingPolicyOutput{Mode: policy.Mode}
	if policy.Mode == ExplicitInterfaceAndLocalIPv4 {
		out.InterfaceID = policy.InterfaceID
		out.LocalIPv4Address = policy.LocalIPv4Address.String()
	}
	return out
}
