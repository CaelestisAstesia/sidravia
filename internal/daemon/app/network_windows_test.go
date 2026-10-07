//go:build windows

package app

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func TestNetworkQueryWindowsRealObserverLoopback(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	output := make(chan environment.Snapshot, 1)
	done := make(chan error, 1)
	go func() { done <- environment.NewSystemObserver().Observe(ctx, output) }()
	finished := false
	defer func() {
		cancel()
		if !finished {
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("observer cleanup failed")
				}
			case <-time.After(3 * time.Second):
				t.Error("observer did not finish after cancellation")
			}
		}
	}()
	var snapshot environment.Snapshot
	select {
	case snapshot = <-output:
	case err := <-done:
		finished = true
		t.Fatalf("real observer failed: %v", err)
	case <-ctx.Done():
		t.Fatal("real observation timed out")
	}
	cancel()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal("observer cancellation failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observer wait timed out")
	}
	if err := setup.application.ApplySystemNetworkSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal("apply real observation failed")
	}
	handler := IPCHandler(setup.application, "v", "b", launchcontract.Headless())
	wire, rpcErr := handler(context.Background(), contract.MethodNetworkInterfaces, []byte(`{}`))
	if rpcErr != nil {
		t.Fatal("real query failed")
	}
	result, err := contract.DecodeNetworkInterfacesResult(wire)
	if err != nil || !result.Available || result.Revision != snapshot.Revision || len(result.Interfaces) != len(snapshot.Interfaces()) {
		t.Fatal("real query lost accepted facts")
	}
	found := false
	for _, iface := range result.Interfaces {
		for _, address := range iface.IPv4Assignments {
			if netip.MustParseAddr(address.Address).IsLoopback() {
				found = true
				if address.AutomaticCandidate || (iface.OperationalState == "up" && !address.ExplicitBindable) {
					t.Fatal("real loopback eligibility incorrect")
				}
			}
		}
	}
	if !found {
		t.Fatal("real Windows observation omitted loopback")
	}
}
