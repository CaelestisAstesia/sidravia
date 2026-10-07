package environment

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

var (
	ErrNetworkBindingUnavailable = errors.New("network binding unavailable")
	ErrNetworkRouteUnavailable   = errors.New("network route unavailable")
)

// NetworkDiagnosticQuery constrains this observation only; it never selects or
// stores a Session binding. InterfaceID and SourceIPv4 must be supplied together.
type NetworkDiagnosticQuery struct {
	DestinationIPv4 netip.Addr
	InterfaceID     InterfaceID
	SourceIPv4      netip.Addr
	Probe           bool
}

// NetworkRoute is an OS proposal, not evidence of a bound protocol socket or
// successful authentication. InterfaceIndex is transient, unlike InterfaceID.
type NetworkRoute struct {
	InterfaceID       InterfaceID
	InterfaceIndex    uint32
	SourceIPv4        netip.Addr
	DestinationPrefix netip.Prefix
	NextHopIPv4       netip.Addr // 0.0.0.0 means on-link.
	RouteMetric       uint32
	InterfaceMetric   uint32
	EffectiveMetric   uint64
}

type NetworkProbeStatus string

const (
	NetworkProbeNotRequested NetworkProbeStatus = "not_requested"
	NetworkProbeReachable    NetworkProbeStatus = "reachable"
	NetworkProbeNoReply      NetworkProbeStatus = "no_reply"
	NetworkProbeUnreachable  NetworkProbeStatus = "unreachable"
	NetworkProbeFailed       NetworkProbeStatus = "failed"
)

type NetworkProbeResult struct {
	Status        NetworkProbeStatus
	RoundTripTime *time.Duration
	Cause         error `json:"-"` // OS cause stays private to the backend boundary.
}

type NetworkDiagnosticResult struct {
	ObservedAt time.Time
	Route      NetworkRoute
	Probe      NetworkProbeResult
}

// DiagnoseNetwork reads fresh platform facts without starting a goroutine.
// Windows probes are synchronous: cancellation can wait for the bounded native
// call (at most 1500ms); it does not interrupt that kernel call immediately.
func DiagnoseNetwork(ctx context.Context, query NetworkDiagnosticQuery) (NetworkDiagnosticResult, error) {
	if ctx == nil {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: nil context")
	}
	if err := ctx.Err(); err != nil {
		return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: context: %w", err)
	}
	if !diagnosticUnicastIPv4(query.DestinationIPv4) {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: invalid destination IPv4")
	}
	if (query.InterfaceID != "") != query.SourceIPv4.IsValid() {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: interface and source must be paired")
	}
	if query.SourceIPv4.IsValid() && !diagnosticUnicastIPv4(query.SourceIPv4) {
		return NetworkDiagnosticResult{}, errors.New("diagnose network: invalid source IPv4")
	}
	return diagnoseNetwork(ctx, query)
}

func diagnosticUnicastIPv4(addr netip.Addr) bool {
	return addr.Is4() && !addr.IsUnspecified() && !addr.IsMulticast() && addr.As4() != [4]byte{255, 255, 255, 255}
}

func effectiveNetworkMetric(route, iface uint32) uint64 {
	return uint64(route) + uint64(iface)
}

// classifyNetworkProbe treats expected network negatives as facts. Unexpected
// native failures also return an error, preserving their original OS cause.
func classifyNetworkProbe(status uint32, cause error) (NetworkProbeResult, error) {
	probe := NetworkProbeResult{Cause: cause}
	switch status {
	case 11010, 258, 1460: // IP_REQ_TIMED_OUT, WAIT_TIMEOUT, ERROR_TIMEOUT
		probe.Status = NetworkProbeNoReply
	case 11002, 11003, 11004, 11005, 11012, 11013, 11014, 11018, 1231, 1232:
		probe.Status = NetworkProbeUnreachable
	default:
		probe.Status = NetworkProbeFailed
		return probe, fmt.Errorf("diagnose network: ICMP failure: %w", cause)
	}
	return probe, nil
}
