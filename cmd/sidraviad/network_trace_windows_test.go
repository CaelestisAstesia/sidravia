//go:build windows

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func TestWindowsTraceReadOnlyLoopbackSnapshot(t *testing.T) {
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	local := socket.LocalAddr().(*net.UDPAddr).AddrPort()
	local = netip.AddrPortFrom(local.Addr().Unmap(), local.Port())
	ctx, cancel := context.WithTimeout(context.Background(), networkTraceTimeout)
	defer cancel()
	result := collectNetworkTrace(ctx, networkTraceRequest{TriggeredAt: time.Now(), Failure: protocol.AuthenticationProtocolNetworkFailure{LocalEndpoint: local, RemoteEndpoint: netip.MustParseAddrPort("127.0.0.1:61440")}})
	if result.Status != "collected" {
		t.Fatalf("snapshot status=%s", result.Status)
	}
	for name, section := range map[string]networkTraceSection{"udp": result.Facts.UDP, "addresses": result.Facts.Addresses, "route": result.Facts.Route, "excludedPorts": result.Facts.ExcludedPorts} {
		if section.Status != "collected" || !json.Valid(section.Data) {
			t.Errorf("%s status=%s data=%s", name, section.Status, section.Data)
		}
	}
	if result.Facts.Events.Status != "collected" {
		t.Errorf("events=%s", result.Facts.Events.Status)
	}
	var owners []struct {
		LocalAddress  string
		LocalPort     uint16
		OwningProcess uint32
	}
	if err := json.Unmarshal(result.Facts.UDP.Data, &owners); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, owner := range owners {
		if owner.LocalAddress == "127.0.0.1" && owner.LocalPort == local.Port() && owner.OwningProcess == uint32(os.Getpid()) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing actual socket owner: %s", result.Facts.UDP.Data)
	}
	var events []struct {
		Channel string
		Status  string
	}
	if err := json.Unmarshal(result.Facts.Events.Data, &events); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Status == "query_failed" {
			t.Fatalf("invalid event query for %s", event.Channel)
		}
	}
	partialCtx, partialCancel := context.WithTimeout(context.Background(), networkTraceTimeout)
	defer partialCancel()
	partial := collectNetworkTrace(partialCtx, networkTraceRequest{TriggeredAt: time.Now(), Failure: protocol.AuthenticationProtocolNetworkFailure{Operation: "set_deadline", LocalEndpoint: local}})
	if partial.Status != "collected" || partial.Facts.Route.Status != "not_applicable" || partial.Facts.UDP.Status != "collected" || partial.Facts.Addresses.Status != "collected" {
		t.Fatalf("partial endpoint snapshot was dropped: %+v", partial)
	}

	if _, err := socket.WriteToUDP([]byte("loopback-only"), socket.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatalf("collector affected bound socket: %v", err)
	}
}

func TestWindowsTraceCancellationIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	result := collectNetworkTrace(ctx, networkTraceRequest{TriggeredAt: start, Failure: protocol.AuthenticationProtocolNetworkFailure{LocalEndpoint: netip.MustParseAddrPort("127.0.0.1:61440"), RemoteEndpoint: netip.MustParseAddrPort("127.0.0.1:61440")}})
	if result.Status != "cancelled_or_timed_out" && result.Status != "start_failed" {
		t.Fatalf("status=%s", result.Status)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("query outlived cancellation bound")
	}
}
