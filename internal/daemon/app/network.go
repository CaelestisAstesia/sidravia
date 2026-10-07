package app

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"time"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/contract"
)

// LatestSystemNetworkSnapshot delegates to the sole accepted-facts owner.
func (application *Application) LatestSystemNetworkSnapshot(ctx context.Context) (environment.Snapshot, bool, error) {
	return application.sup.LatestSystemNetworkSnapshot(ctx)
}

func networkInterfacesResult(snapshot environment.Snapshot, available bool) contract.NetworkInterfacesResult {
	result := contract.NetworkInterfacesResult{Available: available, Interfaces: []contract.NetworkInterfaceResult{}}
	if !available {
		return result
	}
	result.Revision = snapshot.Revision
	observed := snapshot.ObservedAt.Format(time.RFC3339Nano)
	result.ObservedAt = &observed
	interfaces := snapshot.Interfaces()
	slices.SortFunc(interfaces, func(a, b environment.NetworkInterface) int { return cmp.Compare(a.InterfaceID, b.InterfaceID) })
	for _, iface := range interfaces {
		row := contract.NetworkInterfaceResult{InterfaceID: string(iface.InterfaceID), DisplayName: iface.DisplayName, OperationalState: string(iface.OperationalState), PhysicalMedium: string(iface.PhysicalMedium), HardwareBacked: iface.HardwareBacked, PhysicalConnectorPresent: iface.PhysicalConnectorPresent, FilterInterface: iface.FilterInterface, EndpointInterface: iface.EndpointInterface, AddressAssignmentMethod: string(iface.AddressAssignmentMethod), IPv4Assignments: []contract.NetworkIPv4Assignment{}}
		addresses := iface.IPv4AddressAssignments()
		slices.SortFunc(addresses, func(a, b environment.IPv4AddressAssignment) int {
			if order := a.Address.Compare(b.Address); order != 0 {
				return order
			}
			return cmp.Compare(a.PrefixLength, b.PrefixLength)
		})
		for _, assignment := range addresses {
			policy := session.NetworkBindingPolicy{Mode: session.ExplicitInterfaceAndLocalIPv4, InterfaceID: string(iface.InterfaceID), LocalIPv4Address: assignment.Address}
			row.IPv4Assignments = append(row.IPv4Assignments, contract.NetworkIPv4Assignment{Address: assignment.Address.String(), PrefixLength: assignment.PrefixLength, AutomaticCandidate: session.IsAutomaticBindingCandidate(iface, assignment), ExplicitBindable: iface.OperationalState == environment.OperationalStateUp && policy.Validate() == nil})
		}
		result.Interfaces = append(result.Interfaces, row)
	}
	return result
}

func NetworkHandler(application *Application) func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodNetworkInterfaces {
			return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported network method"}
		}
		if err := contract.DecodeEmptyPayload(payload); err != nil {
			return nil, &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "malformed network payload"}
		}
		snapshot, available, err := application.LatestSystemNetworkSnapshot(ctx)
		if err != nil {
			return nil, &contract.Error{Code: contract.ErrorCodeInternalError, Message: "network query failed"}
		}
		result, err := contract.MarshalNetworkInterfacesResult(networkInterfacesResult(snapshot, available))
		if err != nil {
			return nil, &contract.Error{Code: contract.ErrorCodeInternalError, Message: "network query failed"}
		}
		return result, nil
	}
}
