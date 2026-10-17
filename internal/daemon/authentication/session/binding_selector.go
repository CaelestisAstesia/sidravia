package session

import (
	"bytes"
	"fmt"
	"net/netip"
	"slices"
	"time"

	environment "sidravia/internal/daemon/environment"
)

type bindingKey struct {
	interfaceID  environment.InterfaceID
	address      netip.Addr
	prefixLength uint8
}

type automaticBindingSelector struct {
	policy             NetworkBindingPolicy
	lastRevision       uint64
	hasSnapshot        bool
	candidates         []bindingCandidate
	selectedBinding    environment.SelectedSystemNetworkBinding
	hasSelectedBinding bool
}

func newAutomaticBindingSelector() *automaticBindingSelector {
	return &automaticBindingSelector{}
}

// Select is used for explicit bindings and deterministic selection tests.
// Session supplies its own monotonic clock to observe and choose for automation.
func (selector *automaticBindingSelector) Select(snapshot environment.Snapshot) (environment.SelectedSystemNetworkBinding, bool) {
	selector.observe(snapshot, snapshot.ObservedAt)
	candidate, _ := selector.choose(snapshot.ObservedAt, nil, nil, false)
	if candidate == nil {
		return environment.SelectedSystemNetworkBinding{}, false
	}
	return selector.commit(*candidate), true
}

func (selector *automaticBindingSelector) observe(snapshot environment.Snapshot, now time.Time) {
	if selector.hasSnapshot && snapshot.Revision <= selector.lastRevision {
		return
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
					candidates = append(candidates, bindingCandidate{networkInterface: iface, localAddress: assignment, key: bindingKey{iface.InterfaceID, assignment.Address, assignment.PrefixLength}})
				}
			}
		}
	}
	previous := make(map[bindingKey]bindingCandidate, len(selector.candidates))
	for _, candidate := range selector.candidates {
		previous[candidate.key] = candidate
	}
	for i := range candidates {
		if selector.hasSnapshot && selector.policy.Mode != ExplicitInterfaceAndLocalIPv4 {
			candidates[i].eligibleAt = now.Add(bindingStabilityDelay)
			if old, ok := previous[candidates[i].key]; ok {
				candidates[i].eligibleAt = old.eligibleAt
				candidates[i].preferred = old.preferred
			}
		}
	}
	slices.SortFunc(candidates, func(a, b bindingCandidate) int {
		if bindingCandidatePrecedes(a, b) {
			return -1
		}
		if bindingCandidatePrecedes(b, a) {
			return 1
		}
		return 0
	})
	selector.candidates = candidates
	selector.hasSnapshot = true
	selector.lastRevision = snapshot.Revision
}

type bindingCandidate struct {
	networkInterface environment.NetworkInterface
	localAddress     environment.IPv4AddressAssignment
	key              bindingKey
	eligibleAt       time.Time
	preferred        bool
}

// choose reevaluates time against saved facts; it never manufactures a revision.
// The cursor is one previous candidate, not a history of disappeared interfaces.
func (selector *automaticBindingSelector) choose(now time.Time, failures map[environment.InterfaceID]uint8, cursor *bindingCandidate, rotate bool) (*bindingCandidate, time.Time) {
	var retained *bindingCandidate
	if selector.hasSelectedBinding && !rotate {
		for i := range selector.candidates {
			c := &selector.candidates[i]
			if c.key == keyForBinding(selector.selectedBinding) && failures[c.key.interfaceID] < bindingFailureLimit {
				retained = c
				break
			}
		}
	}
	if retained != nil && !retained.eligibleAt.After(now) {
		return retained, retained.eligibleAt
	}
	start := 0
	if cursor != nil {
		found := false
		for i, c := range selector.candidates {
			if c.key.interfaceID == cursor.key.interfaceID {
				start = i + 1
				found = true
			}
		}
		if !found {
			start = len(selector.candidates)
			for i, c := range selector.candidates {
				if bindingInterfacePrecedes(*cursor, c) {
					start = i
					break
				}
			}
		}
		start %= max(1, len(selector.candidates))
	}
	var earliest time.Time
	var pending *bindingCandidate
	for offset := 0; offset < len(selector.candidates); offset++ {
		c := &selector.candidates[(start+offset)%len(selector.candidates)]
		if failures[c.key.interfaceID] >= bindingFailureLimit {
			continue
		}
		// Prefer the address last chosen on this still-present interface. This
		// marker lives only in the current candidate table, not a history cache.
		for i := range selector.candidates {
			preferred := &selector.candidates[i]
			if preferred.key.interfaceID == c.key.interfaceID && preferred.preferred && !preferred.eligibleAt.After(now) {
				return preferred, preferred.eligibleAt
			}
		}
		if !c.eligibleAt.After(now) {
			return c, c.eligibleAt
		}
		if earliest.IsZero() || c.eligibleAt.Before(earliest) {
			earliest = c.eligibleAt
			pending = c
		}
	}
	return pending, earliest
}

func bindingInterfacePrecedes(left, right bindingCandidate) bool {
	if l, r := physicalMediumPriority(left.networkInterface.PhysicalMedium), physicalMediumPriority(right.networkInterface.PhysicalMedium); l != r {
		return l < r
	}
	return left.key.interfaceID < right.key.interfaceID
}

func (selector *automaticBindingSelector) commit(candidate bindingCandidate) environment.SelectedSystemNetworkBinding {
	binding, err := selectBinding([]bindingCandidate{candidate})
	if err != nil {
		panic(err)
	} // Candidate validation is an internal invariant.
	for i := range selector.candidates {
		if selector.candidates[i].key.interfaceID == candidate.key.interfaceID {
			selector.candidates[i].preferred = selector.candidates[i].key == candidate.key
		}
	}
	selector.selectedBinding, selector.hasSelectedBinding = binding, true
	return binding
}

func keyForBinding(binding environment.SelectedSystemNetworkBinding) bindingKey {
	address := binding.LocalIPv4AddressAssignment()
	return bindingKey{binding.NetworkInterface().InterfaceID, address.Address, address.PrefixLength}
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
