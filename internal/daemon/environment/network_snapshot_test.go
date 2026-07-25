package environment

import (
	"net/netip"
	"testing"
	"time"
)

func TestNewNetworkInterfaceRejectsInvalidIPv4Assignments(t *testing.T) {
	testCases := []struct {
		name       string
		assignment IPv4AddressAssignment
	}{
		{name: "invalid address", assignment: IPv4AddressAssignment{PrefixLength: 24}},
		{name: "IPv6 address", assignment: IPv4AddressAssignment{Address: netip.MustParseAddr("2001:db8::1"), PrefixLength: 64}},
		{name: "prefix too long", assignment: IPv4AddressAssignment{Address: netip.MustParseAddr("10.0.0.8"), PrefixLength: 33}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewNetworkInterface(NetworkInterfaceFacts{
				InterfaceID:            "ethernet-1",
				IPv4AddressAssignments: []IPv4AddressAssignment{testCase.assignment},
			})
			if err == nil {
				t.Fatal("expected invalid IPv4 assignment error")
			}
		})
	}
}

func TestNetworkFactsAreIsolatedFromCallerMutation(t *testing.T) {
	selectedAddress := IPv4AddressAssignment{
		Address:      netip.MustParseAddr("10.0.0.8"),
		PrefixLength: 24,
	}
	hardwareAddress := []byte{0, 1, 2, 3, 4, 5}
	assignments := []IPv4AddressAssignment{selectedAddress}
	networkInterface, err := NewNetworkInterface(NetworkInterfaceFacts{
		InterfaceID:            "ethernet-1",
		HardwareAddress:        hardwareAddress,
		IPv4AddressAssignments: assignments,
	})
	if err != nil {
		t.Fatal(err)
	}

	hardwareAddress[0] = 99
	assignments[0].Address = netip.MustParseAddr("192.168.1.8")
	returnedHardwareAddress := networkInterface.HardwareAddress()
	returnedHardwareAddress[1] = 99
	returnedAssignments := networkInterface.IPv4AddressAssignments()
	returnedAssignments[0].Address = netip.MustParseAddr("192.168.1.9")

	if got := networkInterface.HardwareAddress(); got[0] != 0 || got[1] != 1 {
		t.Fatalf("hardware address was mutated through an alias: %v", got)
	}
	if got := networkInterface.IPv4AddressAssignments(); got[0] != selectedAddress {
		t.Fatalf("assignments were mutated through an alias: %+v", got)
	}

	snapshot := NewSnapshot(1, time.Unix(1, 0), []NetworkInterface{networkInterface})
	returnedInterfaces := snapshot.Interfaces()
	returnedInterfaces[0].InterfaceID = "changed"
	if got := snapshot.Interfaces()[0].InterfaceID; got != "ethernet-1" {
		t.Fatalf("snapshot was mutated through returned interfaces: %q", got)
	}
}

func TestSelectedSystemNetworkBindingRequiresAddressFromSelectedInterface(t *testing.T) {
	selectedAddress := IPv4AddressAssignment{
		Address:      netip.MustParseAddr("10.0.0.8"),
		PrefixLength: 24,
	}
	networkInterface, err := NewNetworkInterface(NetworkInterfaceFacts{
		InterfaceID:            "ethernet-1",
		OperationalState:       OperationalStateUp,
		IPv4AddressAssignments: []IPv4AddressAssignment{selectedAddress},
	})
	if err != nil {
		t.Fatal(err)
	}

	binding, err := NewSelectedSystemNetworkBinding(networkInterface, selectedAddress)
	if err != nil {
		t.Fatal(err)
	}
	if binding.LocalIPv4AddressAssignment() != selectedAddress {
		t.Fatalf("unexpected selected address: %+v", binding.LocalIPv4AddressAssignment())
	}
}

func TestSelectedSystemNetworkBindingRejectsAddressFromAnotherInterface(t *testing.T) {
	networkInterface, err := NewNetworkInterface(NetworkInterfaceFacts{
		InterfaceID: "ethernet-1",
		IPv4AddressAssignments: []IPv4AddressAssignment{{
			Address:      netip.MustParseAddr("10.0.0.8"),
			PrefixLength: 24,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	otherAddress := IPv4AddressAssignment{
		Address:      netip.MustParseAddr("192.168.1.8"),
		PrefixLength: 24,
	}

	_, err = NewSelectedSystemNetworkBinding(networkInterface, otherAddress)
	if err == nil {
		t.Fatal("expected address membership error")
	}
}
