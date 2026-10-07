package environment

import (
	"context"
	"errors"
	"math"
	"net/netip"
	"strings"
	"syscall"
	"testing"
)

func TestNetworkDiagnosticsRejectInvalidQueriesBeforePlatform(t *testing.T) {
	valid := netip.MustParseAddr("127.0.0.1")
	tests := []NetworkDiagnosticQuery{
		{},
		{DestinationIPv4: valid, InterfaceID: "private user text"},
		{DestinationIPv4: valid, SourceIPv4: valid},
	}
	for _, text := range []string{"0.0.0.0", "255.255.255.255", "224.0.0.1", "::1", "::ffff:127.0.0.1"} {
		address := netip.MustParseAddr(text)
		tests = append(tests, NetworkDiagnosticQuery{DestinationIPv4: address}, NetworkDiagnosticQuery{DestinationIPv4: valid, InterfaceID: "private user text", SourceIPv4: address})
	}
	for _, query := range tests {
		result, err := DiagnoseNetwork(context.Background(), query)
		if err == nil || errors.Is(err, ErrUnsupported) || result != (NetworkDiagnosticResult{}) {
			t.Fatalf("invalid query reached platform or produced facts: result=%+v err=%v", result, err)
		}
		if strings.Contains(err.Error(), "private user text") {
			t.Fatal("input text leaked into error")
		}
	}
}

func TestNetworkDiagnosticsRejectNilAndCancelledContexts(t *testing.T) {
	query := NetworkDiagnosticQuery{DestinationIPv4: netip.MustParseAddr("127.0.0.1"), Probe: true}
	if result, err := DiagnoseNetwork(nil, query); err == nil || result != (NetworkDiagnosticResult{}) {
		t.Fatalf("nil context: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := DiagnoseNetwork(ctx, query); !errors.Is(err, context.Canceled) || result != (NetworkDiagnosticResult{}) {
		t.Fatalf("cancelled context: %+v %v", result, err)
	}
}

func TestNetworkMetricPreservesFullSum(t *testing.T) {
	if got := effectiveNetworkMetric(math.MaxUint32, math.MaxUint32); got != uint64(math.MaxUint32)*2 {
		t.Fatalf("metric overflow: %d", got)
	}
}

func TestNetworkProbeNegativeFactsPreserveCause(t *testing.T) {
	for _, test := range []struct {
		code    uint32
		status  NetworkProbeStatus
		failure bool
	}{
		{11010, NetworkProbeNoReply, false},
		{11003, NetworkProbeUnreachable, false},
		{11013, NetworkProbeUnreachable, false},
		{8, NetworkProbeFailed, true},
	} {
		cause := syscall.Errno(test.code)
		probe, err := classifyNetworkProbe(test.code, cause)
		if probe.Status != test.status || probe.RoundTripTime != nil || !errors.Is(probe.Cause, cause) || (err != nil) != test.failure {
			t.Fatalf("code %d: %+v %v", test.code, probe, err)
		}
		if test.failure && !errors.Is(err, cause) {
			t.Fatal("failure cause lost")
		}
	}
}
