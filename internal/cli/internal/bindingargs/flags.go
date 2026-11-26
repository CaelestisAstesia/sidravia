package bindingargs

import (
	"errors"

	"sidravia/internal/connection/binding"
	"sidravia/internal/ipc/contract"
)

// Flags retains flag presence so an explicit empty value is invalid.
type Flags struct {
	InterfaceID, LocalIPv4              string
	InterfaceSet, AddressSet, Automatic bool
}

func (flags Flags) Supplied() bool {
	return flags.InterfaceSet || flags.AddressSet || flags.Automatic
}
func (flags Flags) Policy(preserve bool) (*contract.NetworkBindingPolicy, error) {
	if flags.Automatic && (flags.InterfaceSet || flags.AddressSet) {
		return nil, errors.New("--automatic-binding 不能与指定网卡或 IPv4 同时使用")
	}
	if flags.InterfaceSet != flags.AddressSet {
		return nil, errors.New("--interface-id 与 --local-ipv4 必须同时提供")
	}
	if !flags.Supplied() && preserve {
		return nil, nil
	}
	policy := contract.NetworkBindingPolicy{Mode: string(binding.AutomaticallySelectLatestAvailable)}
	if flags.InterfaceSet {
		policy = contract.NetworkBindingPolicy{Mode: string(binding.ExplicitInterfaceAndLocalIPv4), InterfaceID: flags.InterfaceID, LocalIPv4Address: flags.LocalIPv4}
	}
	if _, err := policy.Domain(); err != nil {
		return nil, errors.New("指定网卡 ID 或本地 IPv4 地址无效")
	}
	return &policy, nil
}
