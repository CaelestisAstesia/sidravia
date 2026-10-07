//go:build windows

package environment

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsNetworkDiagnosticsRealLoopback(t *testing.T) {
	destination := netip.MustParseAddr("127.0.0.1")
	result, err := DiagnoseNetwork(context.Background(), NetworkDiagnosticQuery{DestinationIPv4: destination})
	if err != nil {
		t.Fatal(err)
	}
	route := result.Route
	if result.ObservedAt.IsZero() || route.InterfaceID == "" || route.InterfaceIndex == 0 || route.SourceIPv4 != destination || !route.DestinationPrefix.Contains(destination) || route.DestinationPrefix != route.DestinationPrefix.Masked() || !route.NextHopIPv4.Is4() || route.EffectiveMetric != uint64(route.RouteMetric)+uint64(route.InterfaceMetric) {
		t.Fatalf("invalid real loopback route: %+v", result)
	}
	if result.Probe.Status != NetworkProbeNotRequested || result.Probe.RoundTripTime != nil || result.Probe.Cause != nil {
		t.Fatalf("route query requested probe: %+v", result.Probe)
	}
	t.Logf("real default route: id=%q index=%d source=%s prefix=%s nextHop=%s routeMetric=%d interfaceMetric=%d effectiveMetric=%d", route.InterfaceID, route.InterfaceIndex, route.SourceIPv4, route.DestinationPrefix, route.NextHopIPv4, route.RouteMetric, route.InterfaceMetric, route.EffectiveMetric)
	query := NetworkDiagnosticQuery{DestinationIPv4: destination, InterfaceID: route.InterfaceID, SourceIPv4: route.SourceIPv4}
	constrained, err := DiagnoseNetwork(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if constrained.Route.InterfaceID != route.InterfaceID || constrained.Route.InterfaceIndex != route.InterfaceIndex || constrained.Route.SourceIPv4 != route.SourceIPv4 || constrained.Probe.Status != NetworkProbeNotRequested {
		t.Fatalf("constraint ignored: %+v", constrained)
	}
	// Exactly one opt-in native echo, using the route's actual source. No
	// physical adapter, elevation, authentication or campus target is needed.
	query.Probe = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	probed, err := DiagnoseNetwork(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if probed.Probe.Status != NetworkProbeReachable || probed.Probe.RoundTripTime == nil || probed.Probe.Cause != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("bounded loopback echo: %+v elapsed=%v", probed, time.Since(start))
	}
	t.Logf("real source-bound loopback echo: status=%s RTT=%v elapsed=%v", probed.Probe.Status, *probed.Probe.RoundTripTime, time.Since(start))
}

func TestWindowsNetworkDiagnosticsMissingBindingFailsClosed(t *testing.T) {
	result, err := DiagnoseNetwork(context.Background(), NetworkDiagnosticQuery{DestinationIPv4: netip.MustParseAddr("127.0.0.1"), InterfaceID: "sidravia-nonexistent-diagnostic-adapter", SourceIPv4: netip.MustParseAddr("127.0.0.1"), Probe: true})
	if !errors.Is(err, ErrNetworkBindingUnavailable) || result != (NetworkDiagnosticResult{}) {
		t.Fatalf("missing binding fell back: %+v %v", result, err)
	}
}

func TestWindowsNetworkRouteUnavailablePreservesErrno(t *testing.T) {
	err := diagnosticRouteError("read best route", windows.ERROR_NOT_FOUND)
	if !errors.Is(err, ErrNetworkRouteUnavailable) || !errors.Is(err, windows.ERROR_NOT_FOUND) {
		t.Fatalf("route cause lost: %v", err)
	}
}
