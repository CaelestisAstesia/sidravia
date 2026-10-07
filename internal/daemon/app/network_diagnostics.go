package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/contract"
)

// DiagnoseNetwork captures separately owned immutable target and actor facts
// under the operation boundary, then releases it before the bounded OS call.
func (application *Application) DiagnoseNetwork(ctx context.Context, request contract.NetworkDiagnosePayload) (contract.NetworkDiagnoseResult, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return contract.NetworkDiagnoseResult{}, err
	}
	if _, err := contract.DecodeNetworkDiagnosePayload(raw); err != nil {
		return contract.NetworkDiagnoseResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	target, actor, err := application.captureNetworkDiagnostic(ctx, request)
	if err != nil {
		return contract.NetworkDiagnoseResult{}, err
	}
	query, basis := networkDiagnosticSelection(target, actor, request.Probe)
	result := contract.NetworkDiagnoseResult{ObservedAt: time.Now().Format(time.RFC3339Nano), SelectionBasis: basis, Status: "unsupported"}
	if actor != nil {
		result.ProtocolSocket = networkProtocolSocket(actor.ProtocolSocket)
	}
	if !target.Supported {
		reason := "protocol"
		result.UnsupportedReason = &reason
		return result, nil
	}
	address := target.Endpoint.Addr()
	if address.Is4() && !address.IsUnspecified() && !address.IsMulticast() && target.Endpoint.Port() != 0 {
		result.Target = networkEndpoint(target.Endpoint)
	}
	if result.Target == nil || address.String() == "255.255.255.255" {
		reason := "destination"
		result.UnsupportedReason = &reason
		return result, nil
	}
	observed, err := environment.DiagnoseNetwork(ctx, query)
	if err != nil {
		switch {
		case errors.Is(err, environment.ErrUnsupported):
			reason := "platform"
			result.UnsupportedReason = &reason
		case errors.Is(err, environment.ErrNetworkBindingUnavailable):
			result.Status = "binding_unavailable"
		case errors.Is(err, environment.ErrNetworkRouteUnavailable):
			result.Status = "route_unavailable"
		default:
			return contract.NetworkDiagnoseResult{}, fmt.Errorf("network diagnosis: %w", errors.Join(err, context.Cause(ctx)))
		}
		result.ObservedAt = time.Now().Format(time.RFC3339Nano)
		return result, nil
	}
	result.Status = "available"
	result.ObservedAt = observed.ObservedAt.Format(time.RFC3339Nano)
	route := observed.Route
	result.Route = &contract.NetworkDiagnosticRoute{InterfaceID: string(route.InterfaceID), InterfaceIndex: route.InterfaceIndex, SourceIPv4: route.SourceIPv4.String(), DestinationPrefix: route.DestinationPrefix.String(), NextHopIPv4: route.NextHopIPv4.String(), RouteMetric: route.RouteMetric, InterfaceMetric: route.InterfaceMetric, EffectiveMetric: route.EffectiveMetric}
	result.Probe = &contract.NetworkDiagnosticProbe{Status: string(observed.Probe.Status)}
	if observed.Probe.RoundTripTime != nil {
		ms := observed.Probe.RoundTripTime.Milliseconds()
		if ms < 0 || ms > int64(^uint32(0)) {
			return contract.NetworkDiagnoseResult{}, errors.New("invalid network probe time")
		}
		value := uint32(ms)
		result.Probe.RoundTripTimeMs = &value
	}
	return result, nil
}
func (application *Application) captureNetworkDiagnostic(ctx context.Context, request contract.NetworkDiagnosePayload) (session.NetworkDiagnosticTarget, *session.NetworkDiagnosticsSnapshot, error) {
	application.opMu.Lock()
	defer application.opMu.Unlock()
	var target session.NetworkDiagnosticTarget
	var err error
	id := session.AuthenticationSessionID(request.SessionID)
	if request.ConfigurationID != "" {
		target, err = application.authenticationResolver.ResolveNetworkDiagnosticTarget(ctx, config.ConfigurationID(request.ConfigurationID))
		if err != nil {
			return target, nil, err
		}
		application.mu.Lock()
		id = application.sessionsByConfig[config.ConfigurationID(request.ConfigurationID)]
		application.mu.Unlock()
		if id != "" && application.invalidSessions[id] {
			id = ""
		}
	} else {
		if application.invalidSessions[id] {
			return target, nil, supervisor.ErrSessionStateConflict
		}
		target, err = application.sup.GetNetworkDiagnosticTarget(ctx, id)
		if err != nil {
			return target, nil, err
		}
	}
	if id == "" {
		return target, nil, nil
	}
	observed, err := application.sup.GetNetworkDiagnostics(ctx, id)
	if err != nil {
		return target, nil, err
	}
	return target, &observed, nil
}
func networkDiagnosticSelection(target session.NetworkDiagnosticTarget, actor *session.NetworkDiagnosticsSnapshot, probe bool) (environment.NetworkDiagnosticQuery, string) {
	query := environment.NetworkDiagnosticQuery{DestinationIPv4: target.Endpoint.Addr(), Probe: probe}
	if actor != nil && actor.Snapshot.SelectedNetworkBinding != nil {
		binding := actor.Snapshot.SelectedNetworkBinding
		query.InterfaceID, query.SourceIPv4 = binding.InterfaceID, binding.LocalIPv4Address
		return query, "session_binding"
	}
	if target.Policy.Mode == session.ExplicitInterfaceAndLocalIPv4 {
		query.InterfaceID, query.SourceIPv4 = environment.InterfaceID(target.Policy.InterfaceID), target.Policy.LocalIPv4Address
		return query, "configuration_explicit"
	}
	return query, "os_route_proposal"
}
func networkEndpoint(endpoint netip.AddrPort) *contract.NetworkEndpoint {
	return &contract.NetworkEndpoint{Address: endpoint.Addr().String(), Port: endpoint.Port()}
}
func networkProtocolSocket(socket session.ProtocolSocketObservation) *contract.NetworkProtocolSocket {
	result := &contract.NetworkProtocolSocket{State: string(socket.State), RunGeneration: socket.RunGeneration}
	if !socket.UpdatedAt.IsZero() {
		text := socket.UpdatedAt.Format(time.RFC3339Nano)
		result.UpdatedAt = &text
	}
	if socket.State != session.ProtocolSocketNotObserved {
		result.LocalEndpoint, result.RemoteEndpoint = networkEndpoint(socket.LocalEndpoint), networkEndpoint(socket.RemoteEndpoint)
	}
	return result
}
