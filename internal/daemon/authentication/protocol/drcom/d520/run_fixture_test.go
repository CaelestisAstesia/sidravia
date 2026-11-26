package d520

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	credential "sidravia/internal/connection/credential"
	profilevalue "sidravia/internal/connection/profile"
	protocol "sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

// testDurations holds the Profile timeouts used by run tests, kept small so
// failures and heartbeats are observed quickly.
type testDurations struct {
	challenge, login, keepalive, logout, heartbeat, backoffMin, backoffMax time.Duration
}

func defaultTestDurations() testDurations {
	return testDurations{
		challenge:  200 * time.Millisecond,
		login:      200 * time.Millisecond,
		keepalive:  200 * time.Millisecond,
		logout:     100 * time.Millisecond,
		heartbeat:  40 * time.Millisecond,
		backoffMin: 5 * time.Millisecond,
		backoffMax: 10 * time.Millisecond,
	}
}

func testConfigJSON(port int, d testDurations) profilevalue.ProtocolConfiguration {
	jsonStr := fmt.Sprintf(`{
        "serverAddress": "127.0.0.1",
        "serverPort": %d,
        "localPort": {"mode": "system_assigned"},
        "authVersionHex": "2c00",
        "keepAliveVersionHex": "dc02",
        "controlCheckStatusHex": "20",
        "ipdogHex": "01",
        "adapterNumberHex": "01",
        "osInfoHex": "940000000600000000000000280a000002000000",
        "challengePaddingHex": "000000000000000000000000000000",
        "loginIPDogPaddingHex": "00000000",
        "loginDHCPPaddingHex": "0000000000000000",
        "loginAuthExtensionPaddingHex": "0000",
        "challengeTimeout": "%s",
        "loginTimeout": "%s",
        "keepaliveTimeout": "%s",
        "logoutTimeout": "%s",
        "heartbeatInterval": "%s",
        "busyMaxAttempts": 3,
        "busyBackoffMin": "%s",
        "busyBackoffMax": "%s"
    }`, port, d.challenge, d.login, d.keepalive, d.logout, d.heartbeat, d.backoffMin, d.backoffMax)
	return profilevalue.ProtocolConfiguration(jsonStr)
}

func testBinding(t *testing.T) environment.SelectedSystemNetworkBinding {
	t.Helper()
	dhcp := netip.MustParseAddr("127.0.0.1")
	networkInterface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:             "test",
		DisplayName:             "test",
		OperationalState:        environment.OperationalStateUp,
		PhysicalMedium:          environment.PhysicalMediumWired,
		AddressAssignmentMethod: environment.AddressAssignmentDHCP,
		HardwareAddress:         []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("127.0.0.1"), PrefixLength: 8,
		}},
		DNSServerAddresses:    []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")},
		DHCPServerIPv4Address: &dhcp,
	})
	if err != nil {
		t.Fatalf("new network interface: %v", err)
	}
	binding, err := environment.NewSelectedSystemNetworkBinding(networkInterface, environment.IPv4AddressAssignment{
		Address: netip.MustParseAddr("127.0.0.1"), PrefixLength: 8,
	})
	if err != nil {
		t.Fatalf("new binding: %v", err)
	}
	return binding
}

func testHostInformation() environment.SystemHostInformation {
	return environment.SystemHostInformation{
		HostName:               "local-test-host",
		OperatingSystemFamily:  "Windows",
		OperatingSystemRelease: "10",
		MachineArchitecture:    "x86_64",
	}
}

func testCredential() credential.AuthenticationCredential {
	return credential.AuthenticationCredential{
		Username: "student-test",
		Password: "local-test-password",
	}
}

func buildTestRun(t *testing.T, peer *testPeer, d testDurations, cred credential.AuthenticationCredential) protocol.AuthenticationProtocolRun {
	t.Helper()
	factory := NewFactory()
	inputs := protocol.AuthenticationProtocolRunCreationInputs{
		InstitutionProtocolConfiguration: testConfigJSON(peer.port(), d),
		AuthenticationCredential:         cred,
		SelectedSystemNetworkBinding:     testBinding(t),
		SystemHostInformation:            testHostInformation(),
		ProtocolContextOverride:          nil,
	}
	run, err := factory.CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run
}

func startRun(t *testing.T, run protocol.AuthenticationProtocolRun, observer protocol.AuthenticationProtocolRunObserver) (context.Context, context.CancelCauseFunc, <-chan *protocol.AuthenticationProtocolRunFailure) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan *protocol.AuthenticationProtocolRunFailure, 1)
	go func() { done <- run.Execute(ctx, observer) }()
	return ctx, cancel, done
}

func assertRunReturns(t *testing.T, done <-chan *protocol.AuthenticationProtocolRunFailure, wantNil bool, timeout time.Duration) *protocol.AuthenticationProtocolRunFailure {
	t.Helper()
	select {
	case failure := <-done:
		if wantNil && failure != nil {
			t.Fatalf("expected nil failure, got code=%q description=%q recommendation=%q", failure.Code, failure.Description, failure.HandlingRecommendation)
		}
		if !wantNil && failure == nil {
			t.Fatal("expected non-nil failure, got nil")
		}
		return failure
	case <-time.After(timeout):
		t.Fatalf("run did not return within %s", timeout)
		return nil
	}
}

func waitForRequestCount(t *testing.T, peer *testPeer, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(peer.requests()) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("peer received %d requests, want >= %d: ops=%v", len(peer.requests()), want, peer.requestOpNames())
}

func waitForOp(t *testing.T, peer *testPeer, op string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if countOps(peer.requestOpNames(), op) > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("peer did not receive %q within %s: ops=%v", op, timeout, peer.requestOpNames())
}

func hasOpPrefix(ops, prefix []string) bool {
	if len(ops) < len(prefix) {
		return false
	}
	for i, op := range prefix {
		if ops[i] != op {
			return false
		}
	}
	return true
}

func countOps(ops []string, op string) int {
	count := 0
	for _, o := range ops {
		if o == op {
			count++
		}
	}
	return count
}

func ka2RequestSerials(requests [][]byte) []byte {
	out := make([]byte, 0, len(requests))
	for _, req := range requests {
		if isKA2Req(req) {
			out = append(out, req[1])
		}
	}
	return out
}

func ka2Requests(requests [][]byte) [][]byte {
	var out [][]byte
	for _, req := range requests {
		if isKA2Req(req) {
			out = append(out, req)
		}
	}
	return out
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 1. successful Challenge/Login/bootstrap has request order KA1 + 1-1-3, calls
// observer only after bootstrap, then performs one KA1 + 1-3 heartbeat.
