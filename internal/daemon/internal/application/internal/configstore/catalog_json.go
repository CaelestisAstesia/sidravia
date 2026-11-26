package configstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"sort"

	binding "sidravia/internal/connection/binding"
	connectionconfig "sidravia/internal/connection/config"
	profilevalue "sidravia/internal/connection/profile"
	persistence "sidravia/internal/document"
	"sidravia/internal/document/jsondoc"
)

const catalogSchemaVersion uint64 = 4
const catalogSchemaVersion3 uint64 = 3
const catalogSchemaVersion2 uint64 = 2

type catalogDocumentEnvelope struct {
	SchemaVersion  *uint64            `json:"schemaVersion"`
	Configurations *[]json.RawMessage `json:"configurations"`
}
type persistentNetworkBindingPolicy struct {
	Mode             *binding.Mode `json:"mode"`
	InterfaceID      *string       `json:"interfaceId"`
	LocalIPv4Address *string       `json:"localIpv4Address"`
}
type persistentNetworkBindingPolicyOutput struct {
	Mode             binding.Mode `json:"mode"`
	InterfaceID      string       `json:"interfaceId,omitempty"`
	LocalIPv4Address string       `json:"localIpv4Address,omitempty"`
}
type persistentConfiguration struct {
	ConfigurationID         *connectionconfig.ID `json:"configurationId"`
	DisplayName             *string              `json:"displayName"`
	InstitutionProfileID    *profilevalue.ID     `json:"institutionProfileId"`
	Username                *string              `json:"username"`
	Password                *string              `json:"password"`
	NetworkBindingPolicy    json.RawMessage      `json:"networkBindingPolicy"`
	ProtocolContextOverride json.RawMessage      `json:"protocolContextOverride"`
	AutoLogin               *bool                `json:"autoLogin"`
	AutoReconnect           *bool                `json:"autoReconnect"`
}
type persistentConfigurationOutput struct {
	ConfigurationID         connectionconfig.ID                  `json:"configurationId"`
	DisplayName             string                               `json:"displayName"`
	InstitutionProfileID    profilevalue.ID                      `json:"institutionProfileId"`
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

func decodeCatalogDocument(data []byte) (map[connectionconfig.ID]catalogRecord, error) {
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
	if err := decodeCatalogObject(data, &document, []string{"schemaVersion", "configurations"}, pointers...); err != nil {
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
	result := make(map[connectionconfig.ID]catalogRecord, len(*document.Configurations))
	for _, raw := range *document.Configurations {
		var in persistentConfiguration
		if err := decodeCatalogObject(raw, &in, []string{"configurationId", "displayName", "institutionProfileId", "username", "password", "networkBindingPolicy", "protocolContextOverride", "autoLogin", "autoReconnect"}, "/protocolContextOverride"); err != nil {
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
		var storedBinding persistentNetworkBindingPolicy
		if err := decodeCatalogObject(in.NetworkBindingPolicy, &storedBinding, []string{"mode", "interfaceId", "localIpv4Address"}); err != nil || storedBinding.Mode == nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
		}
		policy := binding.Policy{Mode: *storedBinding.Mode}
		var policyFields map[string]json.RawMessage
		if err := json.Unmarshal(in.NetworkBindingPolicy, &policyFields); err != nil {
			return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
		}
		if *document.SchemaVersion != catalogSchemaVersion {
			if len(policyFields) != 1 || storedBinding.InterfaceID != nil || storedBinding.LocalIPv4Address != nil || policy.Mode != binding.AutomaticallySelectLatestAvailable {
				return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
			}
		} else {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(in.NetworkBindingPolicy, &fields); err != nil {
				return nil, persistence.NewFailure(persistence.FailureInvalidDocument, err)
			}
			if policy.Mode == binding.AutomaticallySelectLatestAvailable {
				if len(fields) != 1 {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
			} else {
				if policy.Mode != binding.ExplicitInterfaceAndLocalIPv4 || len(fields) != 3 || storedBinding.InterfaceID == nil || storedBinding.LocalIPv4Address == nil {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
				address, err := netip.ParseAddr(*storedBinding.LocalIPv4Address)
				if err != nil || address.String() != *storedBinding.LocalIPv4Address {
					return nil, persistence.NewFailure(persistence.FailureInvalidDocument, nil)
				}
				policy.InterfaceID = *storedBinding.InterfaceID
				policy.LocalIPv4Address = address
			}
		}
		if err := jsondoc.ValidateOpaqueObjectOrNull(in.ProtocolContextOverride); err != nil {
			return nil, err
		}
		override := profilevalue.ProtocolOverride(nil)
		if !bytes.Equal(bytes.TrimSpace(in.ProtocolContextOverride), []byte("null")) {
			override = append(override, in.ProtocolContextOverride...)
		}
		value := connectionconfig.Configuration{ConfigurationID: *in.ConfigurationID, DisplayName: *in.DisplayName, InstitutionProfileID: *in.InstitutionProfileID, Username: *in.Username, NetworkBindingPolicy: policy, ProtocolContextOverride: override, AutoLogin: autoLogin, AutoReconnect: autoReconnect}
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

func encodeCatalogDocument(records map[connectionconfig.ID]catalogRecord) ([]byte, error) {
	ids := make([]connectionconfig.ID, 0, len(records))
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
	return jsondoc.MarshalDeterministic(catalogDocumentOutput{SchemaVersion: catalogSchemaVersion, Configurations: out})
}

func persistentPolicyOutput(policy binding.Policy) persistentNetworkBindingPolicyOutput {
	out := persistentNetworkBindingPolicyOutput{Mode: policy.Mode}
	if policy.Mode == binding.ExplicitInterfaceAndLocalIPv4 {
		out.InterfaceID = policy.InterfaceID
		out.LocalIPv4Address = policy.LocalIPv4Address.String()
	}
	return out
}

// decodeCatalogObject owns exact catalog wire names. Opaque protocol fields are
// not interpreted here; their existing duplicate/value policy remains separate.
func decodeCatalogObject(data []byte, destination any, allowed []string, opaque ...string) error {
	if e := jsondoc.DecodeStrict(data, destination, opaque...); e != nil {
		return e
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(data, &fields); e != nil || fields == nil {
		return persistence.NewFailure(persistence.FailureInvalidDocument, nil)
	}
	for name := range fields {
		if !slices.Contains(allowed, name) {
			return persistence.NewFailure(persistence.FailureInvalidDocument, nil)
		}
	}
	return nil
}
