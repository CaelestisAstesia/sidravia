//go:build windows

package environment

import (
	"net/netip"
	"slices"

	"golang.org/x/sys/windows"
)

type windowsInterfaceClassification struct {
	hardwareBacked           bool
	physicalConnectorPresent bool
	filterInterface          bool
	endpointInterface        bool
}

func decodeWindowsInterfaceClassification(flags uint8) windowsInterfaceClassification {
	return windowsInterfaceClassification{
		hardwareBacked:           flags&(1<<0) != 0,
		filterInterface:          flags&(1<<1) != 0,
		physicalConnectorPresent: flags&(1<<2) != 0,
		endpointInterface:        flags&(1<<7) != 0,
	}
}

func mapOperationalState(operStatus uint32) OperationalState {
	if operStatus == windows.IfOperStatusUp {
		return OperationalStateUp
	}
	return OperationalStateDown
}

func mapPhysicalMedium(ifType uint32) PhysicalMedium {
	switch ifType {
	case windows.IF_TYPE_ETHERNET_CSMACD:
		return PhysicalMediumWired
	case windows.IF_TYPE_IEEE80211:
		return PhysicalMediumWireless
	default:
		return PhysicalMediumUnknown
	}
}

// collectUnicastAddresses retains usable IPv4 unicast addresses with their
// reported OnLinkPrefixLength and tracks whether any retained origin is DHCP or
// non-manual, so the caller can classify the assignment method.
func collectUnicastAddresses(first *windows.IpAdapterUnicastAddress) (assignments []IPv4AddressAssignment, hasDHCP bool, hasNonManual bool) {
	for ua := first; ua != nil; ua = ua.Next {
		addr, ok := socketAddressToIPv4(ua.Address)
		if !ok || !isUsableUnicastIPv4(addr) {
			continue
		}
		assignments = append(assignments, IPv4AddressAssignment{
			Address:      addr,
			PrefixLength: ua.OnLinkPrefixLength,
		})
		switch ua.PrefixOrigin {
		case windows.IpPrefixOriginDhcp:
			hasDHCP = true
		case windows.IpPrefixOriginManual:
			// static candidate
		default:
			hasNonManual = true
		}
	}
	return assignments, hasDHCP, hasNonManual
}

func collectGatewayIPv4Addresses(first *windows.IpAdapterGatewayAddress) []netip.Addr {
	var addresses []netip.Addr
	for ga := first; ga != nil; ga = ga.Next {
		addr, ok := socketAddressToIPv4(ga.Address)
		if !ok || !isRetainedServiceIPv4(addr) {
			continue
		}
		addresses = append(addresses, addr)
	}
	return addresses
}

func collectDNSIPv4Addresses(first *windows.IpAdapterDnsServerAdapter) []netip.Addr {
	var addresses []netip.Addr
	for da := first; da != nil; da = da.Next {
		addr, ok := socketAddressToIPv4(da.Address)
		if !ok || !isRetainedServiceIPv4(addr) {
			continue
		}
		addresses = append(addresses, addr)
	}
	return addresses
}

func collectDhcpv4Server(sa windows.SocketAddress) *netip.Addr {
	addr, ok := socketAddressToIPv4(sa)
	if !ok || !isRetainedServiceIPv4(addr) {
		return nil
	}
	return &addr
}

func socketAddressToIPv4(sa windows.SocketAddress) (netip.Addr, bool) {
	ip := sa.IP()
	if ip == nil || len(ip) != 4 {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]}), true
}

func isUsableUnicastIPv4(addr netip.Addr) bool {
	return !addr.IsUnspecified() && !addr.IsMulticast()
}

func isRetainedServiceIPv4(addr netip.Addr) bool {
	return !addr.IsUnspecified()
}

func classifyAssignmentMethod(assignments []IPv4AddressAssignment, hasDHCP, hasNonManual bool) AddressAssignmentMethod {
	switch {
	case hasDHCP:
		return AddressAssignmentDHCP
	case len(assignments) > 0 && !hasNonManual:
		return AddressAssignmentStatic
	default:
		return AddressAssignmentUnknown
	}
}

func dedupAddrs(addresses []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(addresses))
	result := make([]netip.Addr, 0, len(addresses))
	for _, addr := range addresses {
		if _, duplicate := seen[addr]; duplicate {
			continue
		}
		seen[addr] = struct{}{}
		result = append(result, addr)
	}
	return result
}

func dedupUnicastAssignments(assignments []IPv4AddressAssignment) []IPv4AddressAssignment {
	seen := make(map[IPv4AddressAssignment]struct{}, len(assignments))
	result := make([]IPv4AddressAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		if _, duplicate := seen[assignment]; duplicate {
			continue
		}
		seen[assignment] = struct{}{}
		result = append(result, assignment)
	}
	return result
}

func sortUnicastAssignments(assignments []IPv4AddressAssignment) {
	slices.SortFunc(assignments, func(a, b IPv4AddressAssignment) int {
		if comparison := a.Address.Compare(b.Address); comparison != 0 {
			return comparison
		}
		return int(a.PrefixLength) - int(b.PrefixLength)
	})
}

func sortAddrs(addresses []netip.Addr) {
	slices.SortFunc(addresses, func(a, b netip.Addr) int {
		return a.Compare(b)
	})
}
