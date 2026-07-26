package session

import (
	"net/netip"
	"testing"
	"time"

	environment "sidravia/internal/daemon/environment"
)

func TestAutomaticBindingSelectorPrefersWiredAtStartup(t *testing.T) {
	selector := newAutomaticBindingSelector()

	binding, ok := selector.Select(snapshot(t, 1,
		newNetworkInterface(t, environment.NetworkInterfaceFacts{
			InterfaceID:              "down-wired",
			OperationalState:         environment.OperationalStateDown,
			PhysicalMedium:           environment.PhysicalMediumWired,
			HardwareBacked:           true,
			PhysicalConnectorPresent: true,
			IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
				Address: netip.MustParseAddr("203.0.113.10"), PrefixLength: 24,
			}},
		}),
		networkInterface(t, "unknown", environment.PhysicalMediumUnknown, "192.0.2.1", 24),
		networkInterface(t, "wireless", environment.PhysicalMediumWireless, "192.0.2.2", 24),
		networkInterface(t, "wired-z", environment.PhysicalMediumWired, "192.0.2.3", 24),
		networkInterface(t, "wired-a", environment.PhysicalMediumWired, "192.0.2.20", 24),
		networkInterface(t, "wired-a", environment.PhysicalMediumWired, "192.0.2.10", 24),
	))

	if !ok {
		t.Fatal("Select() reported no binding")
	}
	assertBinding(t, binding, "wired-a", "192.0.2.10", 24)
}

func TestAutomaticBindingSelectorSwitchesToNewestAvailableNetwork(t *testing.T) {
	t.Run("newer eligible physical candidate wins regardless of physical medium", func(t *testing.T) {
		selector := newAutomaticBindingSelector()
		initial := networkInterface(t, "wired", environment.PhysicalMediumWired, "192.0.2.10", 24)
		newer := networkInterface(t, "wireless", environment.PhysicalMediumWireless, "198.51.100.10", 24)

		binding, ok := selector.Select(snapshot(t, 10, initial))
		if !ok {
			t.Fatal("initial Select() reported no binding")
		}
		assertBinding(t, binding, "wired", "192.0.2.10", 24)

		binding, ok = selector.Select(snapshot(t, 11, initial, newer))
		if !ok {
			t.Fatal("newer Select() reported no binding")
		}
		assertBinding(t, binding, "wireless", "198.51.100.10", 24)
	})

	t.Run("equal revision does not add availability history", func(t *testing.T) {
		selector := newAutomaticBindingSelector()
		initial := networkInterface(t, "initial", environment.PhysicalMediumWired, "192.0.2.10", 24)
		current := networkInterface(t, "current", environment.PhysicalMediumWireless, "198.51.100.10", 24)
		stale := networkInterface(t, "stale", environment.PhysicalMediumWired, "203.0.113.10", 24)
		fresh := networkInterface(t, "fresh", environment.PhysicalMediumUnknown, "203.0.113.20", 24)

		selector.Select(snapshot(t, 10, initial))
		selector.Select(snapshot(t, 11, initial, current))

		binding, ok := selector.Select(snapshot(t, 11, initial, current, stale))
		if !ok {
			t.Fatal("stale Select() reported no retained binding")
		}
		assertBinding(t, binding, "current", "198.51.100.10", 24)

		binding, ok = selector.Select(snapshot(t, 12, initial, current, stale, fresh))
		if !ok {
			t.Fatal("fresh Select() reported no binding")
		}
		assertBinding(t, binding, "stale", "203.0.113.10", 24)
	})

	t.Run("lower revision does not add availability history", func(t *testing.T) {
		selector := newAutomaticBindingSelector()
		initial := networkInterface(t, "initial", environment.PhysicalMediumWired, "192.0.2.10", 24)
		current := networkInterface(t, "current", environment.PhysicalMediumWireless, "198.51.100.10", 24)
		stale := networkInterface(t, "stale", environment.PhysicalMediumWired, "203.0.113.10", 24)
		fresh := networkInterface(t, "fresh", environment.PhysicalMediumUnknown, "203.0.113.20", 24)

		selector.Select(snapshot(t, 10, initial))
		selector.Select(snapshot(t, 11, initial, current))
		selector.Select(snapshot(t, 9, initial, current, stale))

		binding, ok := selector.Select(snapshot(t, 12, initial, current, stale, fresh))
		if !ok {
			t.Fatal("fresh Select() reported no binding")
		}
		assertBinding(t, binding, "stale", "203.0.113.10", 24)
	})
}

func TestAutomaticBindingSelectorIgnoresNewerSoftwareInterface(t *testing.T) {
	selector := newAutomaticBindingSelector()
	physical := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              "physical",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24,
		}},
	})
	software := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:      "software",
		OperationalState: environment.OperationalStateUp,
		PhysicalMedium:   environment.PhysicalMediumWired,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("198.51.100.10"), PrefixLength: 24,
		}},
	})

	binding, ok := selector.Select(snapshot(t, 1, physical))
	if !ok {
		t.Fatal("initial Select() reported no binding")
	}
	assertBinding(t, binding, "physical", "192.0.2.10", 24)

	binding, ok = selector.Select(snapshot(t, 2, physical, software))
	if !ok {
		t.Fatal("newer Select() reported no binding")
	}
	assertBinding(t, binding, "physical", "192.0.2.10", 24)
}

func TestAutomaticBindingSelectorIgnoresHardwareFilterAndEndpointInterfaces(t *testing.T) {
	selector := newAutomaticBindingSelector()
	physical := networkInterface(t, "physical", environment.PhysicalMediumWired, "192.0.2.10", 24)
	filter := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              "filter",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		FilterInterface:          true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("198.51.100.10"), PrefixLength: 24,
		}},
	})
	endpoint := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              "endpoint",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		EndpointInterface:        true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("203.0.113.10"), PrefixLength: 24,
		}},
	})

	selector.Select(snapshot(t, 1, physical))
	binding, ok := selector.Select(snapshot(t, 2, physical, filter, endpoint))
	if !ok {
		t.Fatal("Select() reported no binding")
	}
	assertBinding(t, binding, "physical", "192.0.2.10", 24)
}

func TestAutomaticBindingSelectorReportsNoBindingForIneligibleInterfaces(t *testing.T) {
	selector := newAutomaticBindingSelector()
	software := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              "software",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		PhysicalConnectorPresent: true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24,
		}},
	})
	hardwareWithoutConnector := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:      "hardware-without-connector",
		OperationalState: environment.OperationalStateUp,
		PhysicalMedium:   environment.PhysicalMediumWired,
		HardwareBacked:   true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("198.51.100.10"), PrefixLength: 24,
		}},
	})
	filter := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              "filter",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		FilterInterface:          true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("203.0.113.10"), PrefixLength: 24,
		}},
	})
	endpoint := newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              "endpoint",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		EndpointInterface:        true,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("203.0.113.20"), PrefixLength: 24,
		}},
	})

	if binding, ok := selector.Select(snapshot(
		t,
		1,
		software,
		hardwareWithoutConnector,
		filter,
		endpoint,
	)); ok {
		t.Fatalf("Select() returned ineligible binding: %+v", binding)
	}
}

func TestAutomaticBindingSelectorFallsBackWhenCurrentBindingDisappears(t *testing.T) {
	selector := newAutomaticBindingSelector()
	current := networkInterface(t, "wired", environment.PhysicalMediumWired, "192.0.2.10", 24)
	fallback := networkInterface(t, "wireless", environment.PhysicalMediumWireless, "198.51.100.10", 24)

	selector.Select(snapshot(t, 1, current, fallback))
	binding, ok := selector.Select(snapshot(t, 2, fallback))

	if !ok {
		t.Fatal("Select() reported no fallback binding")
	}
	assertBinding(t, binding, "wireless", "198.51.100.10", 24)
}

func TestAutomaticBindingSelectorTreatsReappearingBindingAsNewest(t *testing.T) {
	selector := newAutomaticBindingSelector()
	stable := networkInterface(t, "wired", environment.PhysicalMediumWired, "192.0.2.10", 24)
	reappearing := networkInterface(t, "wireless", environment.PhysicalMediumWireless, "198.51.100.10", 24)

	selector.Select(snapshot(t, 1, stable, reappearing))
	selector.Select(snapshot(t, 2, stable))
	binding, ok := selector.Select(snapshot(t, 3, stable, reappearing))

	if !ok {
		t.Fatal("Select() reported no reappearing binding")
	}
	assertBinding(t, binding, "wireless", "198.51.100.10", 24)

	t.Run("prefix is part of a binding identity", func(t *testing.T) {
		selector := newAutomaticBindingSelector()
		selector.Select(snapshot(t, 1, stable, reappearing))
		selector.Select(snapshot(t, 2, stable))
		reappearingWithNewPrefix := networkInterface(t, "wireless", environment.PhysicalMediumWireless, "198.51.100.10", 25)

		binding, ok := selector.Select(snapshot(t, 3, stable, reappearingWithNewPrefix))
		if !ok {
			t.Fatal("Select() reported no reappearing binding with a new prefix")
		}
		assertBinding(t, binding, "wireless", "198.51.100.10", 25)
	})
}

func TestBindingAuthenticationFactsIgnoreDisplayName(t *testing.T) {
	left := selectedBinding(t, environment.NetworkInterfaceFacts{
		InterfaceID:                 "ethernet",
		DisplayName:                 "Ethernet",
		OperationalState:            environment.OperationalStateUp,
		PhysicalMedium:              environment.PhysicalMediumWired,
		HardwareBacked:              true,
		PhysicalConnectorPresent:    true,
		HardwareAddress:             []byte{0, 1, 2, 3, 4, 5},
		AddressAssignmentMethod:     environment.AddressAssignmentDHCP,
		IPv4AddressAssignments:      []environment.IPv4AddressAssignment{{Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24}},
		DefaultIPv4GatewayAddresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")},
		DNSServerAddresses:          []netip.Addr{netip.MustParseAddr("192.0.2.53")},
	})
	right := selectedBinding(t, environment.NetworkInterfaceFacts{
		InterfaceID:                 "ethernet",
		DisplayName:                 "Campus Ethernet",
		OperationalState:            environment.OperationalStateUp,
		PhysicalMedium:              environment.PhysicalMediumWired,
		HardwareBacked:              true,
		PhysicalConnectorPresent:    true,
		HardwareAddress:             []byte{0, 1, 2, 3, 4, 5},
		AddressAssignmentMethod:     environment.AddressAssignmentDHCP,
		IPv4AddressAssignments:      []environment.IPv4AddressAssignment{{Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24}},
		DefaultIPv4GatewayAddresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")},
		DNSServerAddresses:          []netip.Addr{netip.MustParseAddr("192.0.2.53")},
	})

	if !bindingsHaveEquivalentAuthenticationFacts(left, right) {
		t.Fatal("bindings with different display names are not equivalent")
	}
}

func TestBindingAuthenticationFactsDetectEveryRelevantChange(t *testing.T) {
	dhcpServer := netip.MustParseAddr("192.0.2.254")
	baseFacts := environment.NetworkInterfaceFacts{
		InterfaceID:              "ethernet",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		HardwareAddress:          []byte{0, 1, 2, 3, 4, 5},
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24,
		}},
		DefaultIPv4GatewayAddresses: []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")},
		DNSServerAddresses:          []netip.Addr{netip.MustParseAddr("192.0.2.53"), netip.MustParseAddr("198.51.100.53")},
		DHCPServerIPv4Address:       &dhcpServer,
	}
	left := selectedBinding(t, baseFacts)

	tests := []struct {
		name   string
		mutate func(*environment.NetworkInterfaceFacts)
	}{
		{
			name: "interface ID",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.InterfaceID = "wireless"
			},
		},
		{
			name: "IPv4 address",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.IPv4AddressAssignments[0].Address = netip.MustParseAddr("192.0.2.11")
			},
		},
		{
			name: "hardware address",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.HardwareAddress = []byte{5, 4, 3, 2, 1, 0}
			},
		},
		{
			name: "physical medium",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.PhysicalMedium = environment.PhysicalMediumWireless
			},
		},
		{
			name: "hardware classification",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.HardwareBacked = false
			},
		},
		{
			name: "physical connector",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.PhysicalConnectorPresent = false
			},
		},
		{
			name: "filter classification",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.FilterInterface = true
			},
		},
		{
			name: "endpoint classification",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.EndpointInterface = true
			},
		},
		{
			name: "address assignment method",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.AddressAssignmentMethod = environment.AddressAssignmentStatic
			},
		},
		{
			name: "prefix length",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.IPv4AddressAssignments[0].PrefixLength = 25
			},
		},
		{
			name: "gateway value",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.DefaultIPv4GatewayAddresses[0] = netip.MustParseAddr("203.0.113.1")
			},
		},
		{
			name: "gateway order",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.DefaultIPv4GatewayAddresses[0], facts.DefaultIPv4GatewayAddresses[1] = facts.DefaultIPv4GatewayAddresses[1], facts.DefaultIPv4GatewayAddresses[0]
			},
		},
		{
			name: "DNS value",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.DNSServerAddresses[0] = netip.MustParseAddr("203.0.113.53")
			},
		},
		{
			name: "DNS order",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.DNSServerAddresses[0], facts.DNSServerAddresses[1] = facts.DNSServerAddresses[1], facts.DNSServerAddresses[0]
			},
		},
		{
			name: "DHCP server",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				changed := netip.MustParseAddr("192.0.2.253")
				facts.DHCPServerIPv4Address = &changed
			},
		},
		{
			name: "DHCP server presence",
			mutate: func(facts *environment.NetworkInterfaceFacts) {
				facts.DHCPServerIPv4Address = nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := cloneNetworkInterfaceFacts(baseFacts)
			test.mutate(&facts)

			if bindingsHaveEquivalentAuthenticationFacts(left, selectedBinding(t, facts)) {
				t.Fatal("bindings with different authentication facts are equivalent")
			}
		})
	}
}

func snapshot(t *testing.T, revision uint64, interfaces ...environment.NetworkInterface) environment.Snapshot {
	t.Helper()
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), interfaces)
}

func networkInterface(
	t *testing.T,
	interfaceID environment.InterfaceID,
	physicalMedium environment.PhysicalMedium,
	address string,
	prefixLength uint8,
) environment.NetworkInterface {
	t.Helper()
	return newNetworkInterface(t, environment.NetworkInterfaceFacts{
		InterfaceID:              interfaceID,
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           physicalMedium,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr(address), PrefixLength: prefixLength,
		}},
	})
}

func selectedBinding(t *testing.T, facts environment.NetworkInterfaceFacts) environment.SelectedSystemNetworkBinding {
	t.Helper()
	networkInterface := newNetworkInterface(t, facts)
	assignment := networkInterface.IPv4AddressAssignments()[0]
	binding, err := environment.NewSelectedSystemNetworkBinding(networkInterface, assignment)
	if err != nil {
		t.Fatalf("NewSelectedSystemNetworkBinding() error = %v", err)
	}
	return binding
}

func newNetworkInterface(t *testing.T, facts environment.NetworkInterfaceFacts) environment.NetworkInterface {
	t.Helper()
	networkInterface, err := environment.NewNetworkInterface(facts)
	if err != nil {
		t.Fatalf("NewNetworkInterface() error = %v", err)
	}
	return networkInterface
}

func assertBinding(
	t *testing.T,
	binding environment.SelectedSystemNetworkBinding,
	wantInterfaceID environment.InterfaceID,
	wantAddress string,
	wantPrefixLength uint8,
) {
	t.Helper()
	if got := binding.NetworkInterface().InterfaceID; got != wantInterfaceID {
		t.Errorf("selected interface ID = %q, want %q", got, wantInterfaceID)
	}
	assignment := binding.LocalIPv4AddressAssignment()
	if want := netip.MustParseAddr(wantAddress); assignment.Address != want {
		t.Errorf("selected address = %s, want %s", assignment.Address, want)
	}
	if assignment.PrefixLength != wantPrefixLength {
		t.Errorf("selected prefix length = %d, want %d", assignment.PrefixLength, wantPrefixLength)
	}
}

func cloneNetworkInterfaceFacts(facts environment.NetworkInterfaceFacts) environment.NetworkInterfaceFacts {
	cloned := facts
	cloned.HardwareAddress = append([]byte(nil), facts.HardwareAddress...)
	cloned.IPv4AddressAssignments = append([]environment.IPv4AddressAssignment(nil), facts.IPv4AddressAssignments...)
	cloned.DefaultIPv4GatewayAddresses = append([]netip.Addr(nil), facts.DefaultIPv4GatewayAddresses...)
	cloned.DNSServerAddresses = append([]netip.Addr(nil), facts.DNSServerAddresses...)
	if facts.DHCPServerIPv4Address != nil {
		address := *facts.DHCPServerIPv4Address
		cloned.DHCPServerIPv4Address = &address
	}
	return cloned
}
