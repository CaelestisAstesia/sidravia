package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"

	"sidravia/internal/connection/binding"
)

// NetworkBindingPolicy is the strict same-build wire value.
type NetworkBindingPolicy struct {
	Mode             string `json:"mode"`
	InterfaceID      string `json:"interfaceId,omitempty"`
	LocalIPv4Address string `json:"localIpv4Address,omitempty"`
}

func (policy NetworkBindingPolicy) Domain() (binding.Policy, error) {
	value := binding.Policy{Mode: binding.Mode(policy.Mode), InterfaceID: policy.InterfaceID}
	if policy.LocalIPv4Address != "" {
		address, err := netip.ParseAddr(policy.LocalIPv4Address)
		if err != nil || address.String() != policy.LocalIPv4Address {
			return value, fmt.Errorf("invalid network binding policy")
		}
		value.LocalIPv4Address = address
	}
	return value, value.Validate()
}
func NetworkBindingPolicyFromDomain(policy binding.Policy) NetworkBindingPolicy {
	value := NetworkBindingPolicy{Mode: string(policy.Mode), InterfaceID: policy.InterfaceID}
	if policy.LocalIPv4Address.IsValid() {
		value.LocalIPv4Address = policy.LocalIPv4Address.String()
	}
	return value
}
func (policy *NetworkBindingPolicy) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("invalid network binding policy")
	}
	fields := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("invalid network binding policy")
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("invalid network binding policy")
		}
		if _, duplicate := fields[name]; duplicate {
			return fmt.Errorf("duplicate network binding policy field")
		}
		if name != "mode" && name != "interfaceId" && name != "localIpv4Address" {
			return fmt.Errorf("unknown network binding policy field")
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return fmt.Errorf("invalid network binding policy")
		}
		var value string
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("invalid network binding policy")
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("invalid network binding policy")
	}
	candidate := NetworkBindingPolicy{Mode: fields["mode"], InterfaceID: fields["interfaceId"], LocalIPv4Address: fields["localIpv4Address"]}
	if candidate.Mode == string(binding.AutomaticallySelectLatestAvailable) {
		if len(fields) != 1 {
			return fmt.Errorf("mixed network binding policy")
		}
	} else if candidate.Mode != string(binding.ExplicitInterfaceAndLocalIPv4) || len(fields) != 3 {
		return fmt.Errorf("missing network binding policy target")
	}
	if _, err := candidate.Domain(); err != nil {
		return fmt.Errorf("invalid network binding policy")
	}
	*policy = candidate
	return nil
}
