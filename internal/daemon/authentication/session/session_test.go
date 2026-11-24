package session

import (
	"bytes"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

func TestSessionMaintainingAuthenticationMovesFromWaitingToAuthenticated(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	if got := sessionSnapshot(t, ctx, session); got.State != WaitingForNetwork {
		t.Fatalf("initial state = %q, want %q", got.State, WaitingForNetwork)
	}
	network := usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet")
	if got, err := session.ApplySystemNetworkSnapshot(ctx, network); err != nil || got.State != Authenticating {
		t.Fatalf("applySystemNetworkSnapshot() = (%q, %v), want authenticating and nil", got.State, err)
	}
	inputs := factory.creationInputs()
	if len(inputs) != 1 {
		t.Fatalf("factory creation count = %d, want 1", len(inputs))
	}
	assertExactFactoryInputs(t, inputs[0], validRuntimeDefinition(t), network)

	if err := factory.run(0).establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	got := waitForState(t, ctx, session, Authenticated)
	if got.LastAuthenticationFailure != nil || got.AuthenticationEstablishedAt == nil {
		t.Fatalf("authenticated snapshot = %#v, want cleared failure and establishment time", got)
	}
}

func TestSessionDoesNotStartProtocolWhileSuspended(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, SuspendAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err != nil || got.State != Suspended {
		t.Fatalf("applySystemNetworkSnapshot() = (%q, %v), want suspended and nil", got.State, err)
	}
	if got := len(factory.creationInputs()); got != 0 {
		t.Fatalf("factory creation count = %d, want 0", got)
	}
}

func TestSessionIgnoresEstablishedEventFromStaleGeneration(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, withStandby(t, usableSystemNetworkSnapshot(t, 1, "first", "First"), "second"))
	first := factory.run(0)
	if err := first.waitForStart(ctx); err != nil {
		t.Fatalf("first run did not start: %v", err)
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second"))
	if cause := first.waitForCancellation(ctx); cause == nil {
		t.Fatal("first run was not cancelled")
	}
	if err := first.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	first.unblock(nil)
	second := waitForFactoryRun(t, ctx, factory, 1)
	got := sessionSnapshot(t, ctx, session)
	if got.State != Authenticating || got.SelectedNetworkBinding.InterfaceID != "second" {
		t.Fatalf("snapshot after stale establishment = %#v", got)
	}
	second.unblock(nil)
}

func TestSessionIgnoresDuplicateAuthenticationEstablished(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err := factory.run(0).establish(ctx); err != nil {
		t.Fatalf("first establish() error = %v", err)
	}
	first := waitForState(t, ctx, session, Authenticated)
	session.post(authenticationEstablishedEvent{generation: 1})
	second := sessionSnapshot(t, ctx, session)
	if second.Revision != first.Revision || second.AuthenticationEstablishedAt == nil || !second.AuthenticationEstablishedAt.Equal(*first.AuthenticationEstablishedAt) {
		t.Fatal("duplicate establishment changed the public snapshot")
	}
}

func TestSessionStartIsIdempotent(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	session.Start()
	session.Start()
	select {
	case event := <-session.RevisionEvents():
		if event.Revision != 1 {
			t.Fatalf("initial revision = %d, want 1", event.Revision)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-session.RevisionEvents():
		t.Fatal("start() published more than one initial revision")
	default:
	}
}

func assertExactFactoryInputs(t *testing.T, got protocol.AuthenticationProtocolRunCreationInputs, definition RuntimeDefinition, network environment.Snapshot) {
	t.Helper()
	if got.AuthenticationCredential.Username != definition.AuthenticationCredential.Username {
		t.Error("factory username differs")
	}
	if got.AuthenticationCredential.Password != definition.AuthenticationCredential.Password {
		t.Error("factory password differs")
	}
	if !bytes.Equal(got.InstitutionProtocolConfiguration, definition.InstitutionProfile.InstitutionProtocolConfiguration) {
		t.Error("factory institution configuration differs")
	}
	if !bytes.Equal(got.ProtocolContextOverride, definition.Configuration.ProtocolContextOverride) {
		t.Error("factory protocol context override differs")
	}
	if got.SystemHostInformation.HostName != definition.SystemHostInformation.HostName {
		t.Error("factory host name differs")
	}
	if got.SystemHostInformation.OperatingSystemFamily != definition.SystemHostInformation.OperatingSystemFamily {
		t.Error("factory operating system family differs")
	}
	if got.SystemHostInformation.OperatingSystemRelease != definition.SystemHostInformation.OperatingSystemRelease {
		t.Error("factory operating system release differs")
	}
	if got.SystemHostInformation.MachineArchitecture != definition.SystemHostInformation.MachineArchitecture {
		t.Error("factory machine architecture differs")
	}
	interfaces := network.Interfaces()
	if len(interfaces) != 1 {
		t.Fatal("test network does not have one interface")
	}
	wantInterface := interfaces[0]
	gotInterface := got.SelectedSystemNetworkBinding.NetworkInterface()
	if gotInterface.InterfaceID != wantInterface.InterfaceID {
		t.Error("factory binding interface ID differs")
	}
	if gotInterface.DisplayName != wantInterface.DisplayName {
		t.Error("factory binding display name differs")
	}
	if gotInterface.OperationalState != wantInterface.OperationalState {
		t.Error("factory binding operational state differs")
	}
	if gotInterface.PhysicalMedium != wantInterface.PhysicalMedium {
		t.Error("factory binding physical medium differs")
	}
	if gotInterface.AddressAssignmentMethod != wantInterface.AddressAssignmentMethod {
		t.Error("factory binding address assignment method differs")
	}
	if !bytes.Equal(gotInterface.HardwareAddress(), wantInterface.HardwareAddress()) {
		t.Error("factory binding hardware address differs")
	}
	if !equalIPv4Assignments(gotInterface.IPv4AddressAssignments(), wantInterface.IPv4AddressAssignments()) {
		t.Error("factory binding IPv4 assignments differ")
	}
	if !equalAddresses(gotInterface.DefaultIPv4GatewayAddresses(), wantInterface.DefaultIPv4GatewayAddresses()) {
		t.Error("factory binding default gateways differ")
	}
	if !equalAddresses(gotInterface.DNSServerAddresses(), wantInterface.DNSServerAddresses()) {
		t.Error("factory binding DNS servers differ")
	}
	gotDHCP, gotHasDHCP := gotInterface.DHCPServerIPv4Address()
	wantDHCP, wantHasDHCP := wantInterface.DHCPServerIPv4Address()
	if gotHasDHCP != wantHasDHCP || (gotHasDHCP && gotDHCP != wantDHCP) {
		t.Error("factory binding DHCP server differs")
	}
	wantAssignment := wantInterface.IPv4AddressAssignments()[0]
	if got.SelectedSystemNetworkBinding.LocalIPv4AddressAssignment() != wantAssignment {
		t.Error("factory binding selected IPv4 assignment differs")
	}
}

func equalIPv4Assignments(left, right []environment.IPv4AddressAssignment) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalAddresses(left, right []netip.Addr) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
