package binding

import (
	"errors"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Mode string

const AutomaticallySelectLatestAvailable Mode = "automatically_select_latest_available"
const ExplicitInterfaceAndLocalIPv4 Mode = "explicit_interface_and_local_ipv4"

type Policy struct {
	Mode             Mode
	InterfaceID      string
	LocalIPv4Address netip.Addr
}

func (policy Policy) Validate() error {
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
