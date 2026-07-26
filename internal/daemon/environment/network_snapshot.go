package environment

import (
	"fmt"
	"net/netip"
	"time"
)

type InterfaceID string
type PhysicalMedium string
type AddressAssignmentMethod string
type OperationalState string

const (
	PhysicalMediumUnknown  PhysicalMedium = "unknown"
	PhysicalMediumWired    PhysicalMedium = "wired"
	PhysicalMediumWireless PhysicalMedium = "wireless"

	AddressAssignmentUnknown AddressAssignmentMethod = "unknown"
	AddressAssignmentStatic  AddressAssignmentMethod = "static"
	AddressAssignmentDHCP    AddressAssignmentMethod = "dhcp"

	OperationalStateDown OperationalState = "down"
	OperationalStateUp   OperationalState = "up"
)

type IPv4AddressAssignment struct {
	Address      netip.Addr
	PrefixLength uint8
}

type NetworkInterfaceFacts struct {
	InterfaceID                 InterfaceID
	DisplayName                 string
	OperationalState            OperationalState
	PhysicalMedium              PhysicalMedium
	HardwareBacked              bool
	PhysicalConnectorPresent    bool
	FilterInterface             bool
	EndpointInterface           bool
	HardwareAddress             []byte
	AddressAssignmentMethod     AddressAssignmentMethod
	IPv4AddressAssignments      []IPv4AddressAssignment
	DefaultIPv4GatewayAddresses []netip.Addr
	DNSServerAddresses          []netip.Addr
	DHCPServerIPv4Address       *netip.Addr
}

type NetworkInterface struct {
	InterfaceID              InterfaceID
	DisplayName              string
	OperationalState         OperationalState
	PhysicalMedium           PhysicalMedium
	HardwareBacked           bool
	PhysicalConnectorPresent bool
	FilterInterface          bool
	EndpointInterface        bool
	AddressAssignmentMethod  AddressAssignmentMethod

	hardwareAddress             []byte
	ipv4AddressAssignments      []IPv4AddressAssignment
	defaultIPv4GatewayAddresses []netip.Addr
	dnsServerAddresses          []netip.Addr
	dhcpServerIPv4Address       *netip.Addr
}

func NewNetworkInterface(facts NetworkInterfaceFacts) (NetworkInterface, error) {
	for _, assignment := range facts.IPv4AddressAssignments {
		if err := validateIPv4AddressAssignment(assignment); err != nil {
			return NetworkInterface{}, fmt.Errorf("network interface %q: %w", facts.InterfaceID, err)
		}
	}
	for _, gateway := range facts.DefaultIPv4GatewayAddresses {
		if !gateway.Is4() {
			return NetworkInterface{}, fmt.Errorf(
				"network interface %q has invalid IPv4 gateway %q",
				facts.InterfaceID,
				gateway,
			)
		}
	}
	for _, dnsServer := range facts.DNSServerAddresses {
		if !dnsServer.IsValid() {
			return NetworkInterface{}, fmt.Errorf(
				"network interface %q has invalid DNS server address",
				facts.InterfaceID,
			)
		}
	}
	if facts.DHCPServerIPv4Address != nil && !facts.DHCPServerIPv4Address.Is4() {
		return NetworkInterface{}, fmt.Errorf(
			"network interface %q has invalid DHCP server IPv4 address %q",
			facts.InterfaceID,
			*facts.DHCPServerIPv4Address,
		)
	}

	return NetworkInterface{
		InterfaceID:                 facts.InterfaceID,
		DisplayName:                 facts.DisplayName,
		OperationalState:            facts.OperationalState,
		PhysicalMedium:              facts.PhysicalMedium,
		HardwareBacked:              facts.HardwareBacked,
		PhysicalConnectorPresent:    facts.PhysicalConnectorPresent,
		FilterInterface:             facts.FilterInterface,
		EndpointInterface:           facts.EndpointInterface,
		AddressAssignmentMethod:     facts.AddressAssignmentMethod,
		hardwareAddress:             cloneSlice(facts.HardwareAddress),
		ipv4AddressAssignments:      cloneSlice(facts.IPv4AddressAssignments),
		defaultIPv4GatewayAddresses: cloneSlice(facts.DefaultIPv4GatewayAddresses),
		dnsServerAddresses:          cloneSlice(facts.DNSServerAddresses),
		dhcpServerIPv4Address:       cloneAddressPointer(facts.DHCPServerIPv4Address),
	}, nil
}

func (networkInterface NetworkInterface) HardwareAddress() []byte {
	return cloneSlice(networkInterface.hardwareAddress)
}

func (networkInterface NetworkInterface) IPv4AddressAssignments() []IPv4AddressAssignment {
	return cloneSlice(networkInterface.ipv4AddressAssignments)
}

func (networkInterface NetworkInterface) DefaultIPv4GatewayAddresses() []netip.Addr {
	return cloneSlice(networkInterface.defaultIPv4GatewayAddresses)
}

func (networkInterface NetworkInterface) DNSServerAddresses() []netip.Addr {
	return cloneSlice(networkInterface.dnsServerAddresses)
}

func (networkInterface NetworkInterface) DHCPServerIPv4Address() (netip.Addr, bool) {
	if networkInterface.dhcpServerIPv4Address == nil {
		return netip.Addr{}, false
	}
	return *networkInterface.dhcpServerIPv4Address, true
}

func (networkInterface NetworkInterface) clone() NetworkInterface {
	networkInterface.hardwareAddress = cloneSlice(networkInterface.hardwareAddress)
	networkInterface.ipv4AddressAssignments = cloneSlice(networkInterface.ipv4AddressAssignments)
	networkInterface.defaultIPv4GatewayAddresses = cloneSlice(networkInterface.defaultIPv4GatewayAddresses)
	networkInterface.dnsServerAddresses = cloneSlice(networkInterface.dnsServerAddresses)
	networkInterface.dhcpServerIPv4Address = cloneAddressPointer(networkInterface.dhcpServerIPv4Address)
	return networkInterface
}

type Snapshot struct {
	Revision   uint64
	ObservedAt time.Time

	interfaces []NetworkInterface
}

func NewSnapshot(revision uint64, observedAt time.Time, interfaces []NetworkInterface) Snapshot {
	immutableInterfaces := make([]NetworkInterface, len(interfaces))
	for index, networkInterface := range interfaces {
		immutableInterfaces[index] = networkInterface.clone()
	}
	return Snapshot{
		Revision:   revision,
		ObservedAt: observedAt,
		interfaces: immutableInterfaces,
	}
}

func (snapshot Snapshot) Interfaces() []NetworkInterface {
	interfaces := make([]NetworkInterface, len(snapshot.interfaces))
	for index, networkInterface := range snapshot.interfaces {
		interfaces[index] = networkInterface.clone()
	}
	return interfaces
}

type SelectedSystemNetworkBinding struct {
	networkInterface           NetworkInterface
	localIPv4AddressAssignment IPv4AddressAssignment
}

func NewSelectedSystemNetworkBinding(
	networkInterface NetworkInterface,
	localAddress IPv4AddressAssignment,
) (SelectedSystemNetworkBinding, error) {
	if err := validateIPv4AddressAssignment(localAddress); err != nil {
		return SelectedSystemNetworkBinding{}, err
	}
	for _, candidate := range networkInterface.ipv4AddressAssignments {
		if candidate == localAddress {
			return SelectedSystemNetworkBinding{
				networkInterface:           networkInterface.clone(),
				localIPv4AddressAssignment: localAddress,
			}, nil
		}
	}
	return SelectedSystemNetworkBinding{}, fmt.Errorf(
		"IPv4 address %s/%d does not belong to network interface %q",
		localAddress.Address,
		localAddress.PrefixLength,
		networkInterface.InterfaceID,
	)
}

func (binding SelectedSystemNetworkBinding) NetworkInterface() NetworkInterface {
	return binding.networkInterface.clone()
}

func (binding SelectedSystemNetworkBinding) LocalIPv4AddressAssignment() IPv4AddressAssignment {
	return binding.localIPv4AddressAssignment
}

func validateIPv4AddressAssignment(assignment IPv4AddressAssignment) error {
	if !assignment.Address.Is4() {
		return fmt.Errorf("invalid IPv4 address %q", assignment.Address)
	}
	if assignment.PrefixLength > 32 {
		return fmt.Errorf(
			"invalid IPv4 prefix length %d for address %s",
			assignment.PrefixLength,
			assignment.Address,
		)
	}
	return nil
}

func cloneAddressPointer(address *netip.Addr) *netip.Addr {
	if address == nil {
		return nil
	}
	clonedAddress := *address
	return &clonedAddress
}

func cloneSlice[T any](values []T) []T {
	if values == nil {
		return nil
	}
	return append([]T(nil), values...)
}
