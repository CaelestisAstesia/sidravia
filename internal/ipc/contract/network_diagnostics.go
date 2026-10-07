package contract

import (
	"encoding/json"
	"errors"
	"net/netip"
	"time"
)

const MethodNetworkDiagnose = "network.diagnose"

type NetworkDiagnosePayload struct {
	ConfigurationID string `json:"configurationId,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	Probe           bool   `json:"probe,omitempty"`
}

func DecodeNetworkDiagnosePayload(data []byte) (NetworkDiagnosePayload, error) {
	if err := networkObject(data, nil, "configurationId", "sessionId", "probe"); err != nil {
		return NetworkDiagnosePayload{}, err
	}
	var value NetworkDiagnosePayload
	if err := decodeStrict(data, &value); err != nil {
		return value, err
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(data, &fields); err != nil {
		return value, err
	}
	_, config := fields["configurationId"]
	_, session := fields["sessionId"]
	if config == session || config && value.ConfigurationID == "" || session && value.SessionID == "" {
		return value, errors.New("invalid network selector")
	}
	return value, nil
}

type NetworkEndpoint struct {
	Address string `json:"address"`
	Port    uint16 `json:"port"`
}
type NetworkDiagnosticRoute struct {
	InterfaceID       string `json:"interfaceId"`
	InterfaceIndex    uint32 `json:"interfaceIndex"`
	SourceIPv4        string `json:"sourceIPv4"`
	DestinationPrefix string `json:"destinationPrefix"`
	NextHopIPv4       string `json:"nextHopIPv4"`
	RouteMetric       uint32 `json:"routeMetric"`
	InterfaceMetric   uint32 `json:"interfaceMetric"`
	EffectiveMetric   uint64 `json:"effectiveMetric"`
}
type NetworkDiagnosticProbe struct {
	Status          string  `json:"status"`
	RoundTripTimeMs *uint32 `json:"roundTripTimeMs,omitempty"`
}
type NetworkProtocolSocket struct {
	State          string           `json:"state"`
	RunGeneration  uint64           `json:"runGeneration"`
	UpdatedAt      *string          `json:"updatedAt,omitempty"`
	LocalEndpoint  *NetworkEndpoint `json:"localEndpoint,omitempty"`
	RemoteEndpoint *NetworkEndpoint `json:"remoteEndpoint,omitempty"`
}
type NetworkDiagnoseResult struct {
	ObservedAt        string                  `json:"observedAt"`
	SelectionBasis    string                  `json:"selectionBasis"`
	Status            string                  `json:"status"`
	UnsupportedReason *string                 `json:"unsupportedReason,omitempty"`
	Target            *NetworkEndpoint        `json:"target,omitempty"`
	Route             *NetworkDiagnosticRoute `json:"route,omitempty"`
	Probe             *NetworkDiagnosticProbe `json:"probe,omitempty"`
	ProtocolSocket    *NetworkProtocolSocket  `json:"protocolSocket,omitempty"`
}

func networkTime(text string) bool {
	if !networkTimePattern.MatchString(text) {
		return false
	}
	observed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || observed.IsZero() {
		return false
	}
	if text[len(text)-1] != 'Z' {
		offset := text[len(text)-6:]
		if offset[1:3] > "23" || offset[4:6] > "59" {
			return false
		}
	}
	return true
}
func networkIPv4(text string, zero, broadcast bool) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(text)
	return addr, err == nil && addr.Is4() && addr.String() == text && !addr.IsMulticast() && (zero || !addr.IsUnspecified()) && (broadcast || text != "255.255.255.255")
}
func (value *NetworkEndpoint) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"address", "port"}); err != nil {
		return err
	}
	type plain NetworkEndpoint
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if _, ok := networkIPv4(wire.Address, false, true); !ok || wire.Port == 0 {
		return errors.New("invalid network endpoint")
	}
	*value = NetworkEndpoint(wire)
	return nil
}
func (value *NetworkDiagnosticRoute) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"interfaceId", "interfaceIndex", "sourceIPv4", "destinationPrefix", "nextHopIPv4", "routeMetric", "interfaceMetric", "effectiveMetric"}); err != nil {
		return err
	}
	type plain NetworkDiagnosticRoute
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.InterfaceID == "" || wire.InterfaceIndex == 0 {
		return errors.New("invalid network route interface")
	}
	if _, ok := networkIPv4(wire.SourceIPv4, false, false); !ok {
		return errors.New("invalid network route source")
	}
	prefix, err := netip.ParsePrefix(wire.DestinationPrefix)
	if err != nil || !prefix.Addr().Is4() || prefix.Masked() != prefix || prefix.String() != wire.DestinationPrefix {
		return errors.New("invalid network route prefix")
	}
	if _, ok := networkIPv4(wire.NextHopIPv4, true, false); !ok {
		return errors.New("invalid network next hop")
	}
	if wire.EffectiveMetric != uint64(wire.RouteMetric)+uint64(wire.InterfaceMetric) {
		return errors.New("invalid network metric sum")
	}
	*value = NetworkDiagnosticRoute(wire)
	return nil
}
func (value *NetworkDiagnosticProbe) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"status"}, "roundTripTimeMs"); err != nil {
		return err
	}
	type plain NetworkDiagnosticProbe
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	switch wire.Status {
	case "reachable":
		if wire.RoundTripTimeMs == nil {
			return errors.New("missing network RTT")
		}
	case "not_requested", "no_reply", "unreachable", "failed":
		if wire.RoundTripTimeMs != nil {
			return errors.New("unexpected network RTT")
		}
	default:
		return errors.New("invalid network probe status")
	}
	*value = NetworkDiagnosticProbe(wire)
	return nil
}
func (value *NetworkProtocolSocket) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"state", "runGeneration"}, "updatedAt", "localEndpoint", "remoteEndpoint"); err != nil {
		return err
	}
	type plain NetworkProtocolSocket
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.State == "not_observed" {
		if wire.LocalEndpoint != nil || wire.RemoteEndpoint != nil || (wire.RunGeneration == 0) != (wire.UpdatedAt == nil) {
			return errors.New("invalid unobserved socket")
		}
	} else {
		switch wire.State {
		case "open", "closed", "close_failed", "close_unconfirmed":
		default:
			return errors.New("invalid socket state")
		}
		if wire.RunGeneration == 0 || wire.UpdatedAt == nil || wire.LocalEndpoint == nil || wire.RemoteEndpoint == nil || wire.LocalEndpoint.Address == "255.255.255.255" {
			return errors.New("invalid observed socket")
		}
	}
	if wire.UpdatedAt != nil && !networkTime(*wire.UpdatedAt) {
		return errors.New("invalid socket time")
	}
	*value = NetworkProtocolSocket(wire)
	return nil
}
func (value *NetworkDiagnoseResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"observedAt", "selectionBasis", "status"}, "unsupportedReason", "target", "route", "probe", "protocolSocket"); err != nil {
		return err
	}
	type plain NetworkDiagnoseResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if !networkTime(wire.ObservedAt) {
		return errors.New("invalid diagnosis time")
	}
	switch wire.SelectionBasis {
	case "session_binding", "configuration_explicit", "os_route_proposal":
	default:
		return errors.New("invalid selection basis")
	}
	switch wire.Status {
	case "available":
		if wire.Target == nil || wire.Route == nil || wire.Probe == nil || wire.UnsupportedReason != nil {
			return errors.New("invalid available diagnosis")
		}
		prefix, _ := netip.ParsePrefix(wire.Route.DestinationPrefix)
		target, _ := netip.ParseAddr(wire.Target.Address)
		if !prefix.Contains(target) {
			return errors.New("route does not contain target")
		}
	case "unsupported":
		if wire.Route != nil || wire.Probe != nil || wire.UnsupportedReason == nil {
			return errors.New("invalid unsupported diagnosis")
		}
		switch *wire.UnsupportedReason {
		case "platform":
			if wire.Target == nil {
				return errors.New("missing platform target")
			}
		case "protocol", "destination":
		default:
			return errors.New("invalid unsupported reason")
		}
	case "binding_unavailable", "route_unavailable":
		if wire.Target == nil || wire.Route != nil || wire.Probe != nil || wire.UnsupportedReason != nil {
			return errors.New("invalid unavailable diagnosis")
		}
	default:
		return errors.New("invalid diagnosis status")
	}
	if wire.Target != nil && wire.Target.Address == "255.255.255.255" && !(wire.Status == "unsupported" && *wire.UnsupportedReason == "destination") {
		return errors.New("broadcast diagnosis target")
	}
	*value = NetworkDiagnoseResult(wire)
	return nil
}
func DecodeNetworkDiagnoseResult(data []byte) (NetworkDiagnoseResult, error) {
	var value NetworkDiagnoseResult
	err := decodeStrict(data, &value)
	return value, err
}
func MarshalNetworkDiagnoseResult(value NetworkDiagnoseResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeNetworkDiagnoseResult(data); err != nil {
		return nil, err
	}
	return data, nil
}
