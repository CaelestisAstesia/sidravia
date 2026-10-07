package session

import (
	"bytes"
	"fmt"
	"net/netip"
	"slices"

	environment "sidravia/internal/daemon/environment"
)

type bindingKey struct {
	interfaceID  environment.InterfaceID
	address      netip.Addr
	prefixLength uint8
}

type automaticBindingSelector struct {
	policy             NetworkBindingPolicy
	firstAvailableAt   map[bindingKey]uint64
	lastRevision       uint64
	hasSnapshot        bool
	selectedBinding    environment.SelectedSystemNetworkBinding
	hasSelectedBinding bool
}

func newAutomaticBindingSelector() *automaticBindingSelector {
	return &automaticBindingSelector{
		firstAvailableAt: make(map[bindingKey]uint64),
	}
}

func (selector *automaticBindingSelector) Select(
	snapshot environment.Snapshot,
) (environment.SelectedSystemNetworkBinding, bool) {
	if selector.hasSnapshot && snapshot.Revision <= selector.lastRevision {
		return selector.selectedBinding, selector.hasSelectedBinding
	}

	candidates := availableBindingCandidates(snapshot)
	if selector.policy.Mode == ExplicitInterfaceAndLocalIPv4 {
		candidates = nil
		for _, iface := range snapshot.Interfaces() {
			if string(iface.InterfaceID) != selector.policy.InterfaceID || iface.OperationalState != environment.OperationalStateUp {
				continue
			}
			for _, assignment := range iface.IPv4AddressAssignments() {
				if assignment.Address == selector.policy.LocalIPv4Address {
					candidates = append(candidates, bindingCandidate{networkInterface: iface, localAddress: assignment, key: bindingKey{interfaceID: iface.InterfaceID, address: assignment.Address, prefixLength: assignment.PrefixLength}})
				}
			}
		}
	}
	if selector.policy.Mode == ExplicitInterfaceAndLocalIPv4 {
		for index := range candidates {
			candidates[index].firstAvailableAt = 0
		}
	} else {
		availableKeys := make(map[bindingKey]struct{}, len(candidates))
		for _, candidate := range candidates {
			availableKeys[candidate.key] = struct{}{}
		}
		for key := range selector.firstAvailableAt {
			if _, available := availableKeys[key]; !available {
				delete(selector.firstAvailableAt, key)
			}
		}
		for index := range candidates {
			if _, known := selector.firstAvailableAt[candidates[index].key]; !known {
				selector.firstAvailableAt[candidates[index].key] = snapshot.Revision
			}
			candidates[index].firstAvailableAt = selector.firstAvailableAt[candidates[index].key]
		}
	}

	selector.lastRevision = snapshot.Revision
	selector.hasSnapshot = true
	if len(candidates) == 0 {
		selector.selectedBinding = environment.SelectedSystemNetworkBinding{}
		selector.hasSelectedBinding = false
		return environment.SelectedSystemNetworkBinding{}, false
	}

	selected, err := selectBinding(candidates)
	if err != nil {
		panic(err)
	}
	selector.selectedBinding = selected
	selector.hasSelectedBinding = true
	return selected, true
}

type bindingCandidate struct {
	networkInterface environment.NetworkInterface
	localAddress     environment.IPv4AddressAssignment
	key              bindingKey
	firstAvailableAt uint64
}

func availableBindingCandidates(snapshot environment.Snapshot) []bindingCandidate {
	var candidates []bindingCandidate
	for _, networkInterface := range snapshot.Interfaces() {
		for _, assignment := range networkInterface.IPv4AddressAssignments() {
			if !IsAutomaticBindingCandidate(networkInterface, assignment) {
				continue
			}
			candidates = append(candidates, bindingCandidate{
				networkInterface: networkInterface,
				localAddress:     assignment,
				key: bindingKey{
					interfaceID:  networkInterface.InterfaceID,
					address:      assignment.Address,
					prefixLength: assignment.PrefixLength,
				},
			})
		}
	}
	return candidates
}

func selectBinding(candidates []bindingCandidate) (environment.SelectedSystemNetworkBinding, error) {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if bindingCandidatePrecedes(candidate, best) {
			best = candidate
		}
	}
	binding, err := environment.NewSelectedSystemNetworkBinding(best.networkInterface, best.localAddress)
	if err != nil {
		return environment.SelectedSystemNetworkBinding{}, fmt.Errorf(
			"select available network binding: internal invariant violated: %w",
			err,
		)
	}
	return binding, nil
}

func bindingCandidatePrecedes(left, right bindingCandidate) bool {
	if left.firstAvailableAt != right.firstAvailableAt {
		return left.firstAvailableAt > right.firstAvailableAt
	}
	if leftMedium, rightMedium := physicalMediumPriority(left.networkInterface.PhysicalMedium), physicalMediumPriority(right.networkInterface.PhysicalMedium); leftMedium != rightMedium {
		return leftMedium < rightMedium
	}
	if left.networkInterface.InterfaceID != right.networkInterface.InterfaceID {
		return left.networkInterface.InterfaceID < right.networkInterface.InterfaceID
	}
	if addressComparison := left.localAddress.Address.Compare(right.localAddress.Address); addressComparison != 0 {
		return addressComparison < 0
	}
	return left.localAddress.PrefixLength < right.localAddress.PrefixLength
}

func physicalMediumPriority(physicalMedium environment.PhysicalMedium) int {
	switch physicalMedium {
	case environment.PhysicalMediumWired:
		return 0
	case environment.PhysicalMediumWireless:
		return 1
	default:
		return 2
	}
}

func bindingsHaveEquivalentAuthenticationFacts(
	left environment.SelectedSystemNetworkBinding,
	right environment.SelectedSystemNetworkBinding,
) bool {
	leftInterface := left.NetworkInterface()
	rightInterface := right.NetworkInterface()
	leftAddress := left.LocalIPv4AddressAssignment()
	rightAddress := right.LocalIPv4AddressAssignment()
	if leftInterface.InterfaceID != rightInterface.InterfaceID ||
		leftAddress != rightAddress ||
		leftInterface.PhysicalMedium != rightInterface.PhysicalMedium ||
		leftInterface.HardwareBacked != rightInterface.HardwareBacked ||
		leftInterface.PhysicalConnectorPresent != rightInterface.PhysicalConnectorPresent ||
		leftInterface.FilterInterface != rightInterface.FilterInterface ||
		leftInterface.EndpointInterface != rightInterface.EndpointInterface ||
		leftInterface.AddressAssignmentMethod != rightInterface.AddressAssignmentMethod ||
		!bytes.Equal(leftInterface.HardwareAddress(), rightInterface.HardwareAddress()) ||
		!slices.Equal(leftInterface.DefaultIPv4GatewayAddresses(), rightInterface.DefaultIPv4GatewayAddresses()) ||
		!slices.Equal(leftInterface.DNSServerAddresses(), rightInterface.DNSServerAddresses()) {
		return false
	}

	leftDHCPServer, leftHasDHCPServer := leftInterface.DHCPServerIPv4Address()
	rightDHCPServer, rightHasDHCPServer := rightInterface.DHCPServerIPv4Address()
	return leftHasDHCPServer == rightHasDHCPServer &&
		(!leftHasDHCPServer || leftDHCPServer == rightDHCPServer)
}

func newPolicyBindingSelector(policy NetworkBindingPolicy) *automaticBindingSelector {
	selector := newAutomaticBindingSelector()
	selector.policy = policy
	return selector
}

// IsAutomaticBindingCandidate is the shared automatic selection eligibility
// rule. The selector still owns availability history and candidate ordering.
func IsAutomaticBindingCandidate(iface environment.NetworkInterface, assignment environment.IPv4AddressAssignment) bool {
	return iface.OperationalState == environment.OperationalStateUp &&
		iface.HardwareBacked && iface.PhysicalConnectorPresent && !iface.FilterInterface && !iface.EndpointInterface && !assignment.Address.IsLoopback()
}
