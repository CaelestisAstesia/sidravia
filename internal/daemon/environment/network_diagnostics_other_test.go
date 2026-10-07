//go:build !windows

package environment

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestNetworkDiagnosticsUnsupportedWithoutFakeFacts(t *testing.T) {
	for _, probe := range []bool{false, true} {
		result, err := DiagnoseNetwork(context.Background(), NetworkDiagnosticQuery{DestinationIPv4: netip.MustParseAddr("127.0.0.1"), Probe: probe})
		if !errors.Is(err, ErrUnsupported) || err == ErrUnsupported || result != (NetworkDiagnosticResult{}) {
			t.Fatalf("unsupported platform: %+v %v", result, err)
		}
	}
}
