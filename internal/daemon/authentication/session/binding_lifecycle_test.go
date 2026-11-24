package session

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

func TestSessionKeepsRunWhenOnlyNetworkDisplayNameChanges(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err := factory.run(0).establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	before := waitForState(t, ctx, session, Authenticated)
	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Campus Ethernet"))
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	if len(factory.creationInputs()) != 1 || got.SelectedNetworkBinding.DisplayName != "Campus Ethernet" {
		t.Fatalf("display-only update = %#v; factory calls = %d", got, len(factory.creationInputs()))
	}
	if got.State != Authenticated || got.AuthenticationEstablishedAt == nil || !got.AuthenticationEstablishedAt.Equal(*before.AuthenticationEstablishedAt) {
		t.Fatalf("display-only update cleared authentication: before=%#v after=%#v", before, got)
	}
}

func TestSessionPublishesReauthenticationBeforeChangedBindingCleanup(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, withStandby(t, usableSystemNetworkSnapshot(t, 1, "first", "First"), "second"))
	first := factory.run(0)
	defer first.unblock(nil)
	if err := first.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	authenticated := waitForState(t, ctx, session, Authenticated)
	if authenticated.AuthenticationEstablishedAt == nil {
		t.Fatal("first binding was not authenticated")
	}

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second"))
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	if got.SelectedNetworkBinding == nil || got.SelectedNetworkBinding.InterfaceID != "second" || got.State != Authenticating || got.StateReason != nil || got.AuthenticationEstablishedAt != nil {
		t.Fatalf("changed binding snapshot before cleanup = %#v", got)
	}
	assertNextRevision(t, "atomic authenticated binding reauthentication", got, authenticated)
	assertCleanupRequirement(t, first.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("changed binding overlapped runs: calls before cleanup = %d", calls)
	}

	first.unblock(blockingCommandFailure("canceled-old-binding"))
	second := waitForFactoryRun(t, ctx, factory, 1)
	if inputs := factory.creationInputs(); inputs[1].SelectedSystemNetworkBinding.NetworkInterface().InterfaceID != "second" {
		t.Fatalf("replacement run used wrong binding: %#v", inputs[1].SelectedSystemNetworkBinding)
	}
	second.unblock(nil)
}

func TestSessionWaitsForCanceledRunBeforeStartingReplacement(t *testing.T) {
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
	if got := len(factory.creationInputs()); got != 1 {
		t.Fatalf("factory creation count before first run finishes = %d, want 1", got)
	}
	first.unblock(nil)
	waitForFactoryRun(t, ctx, factory, 1).unblock(nil)
}

func TestSessionIgnoresStaleNetworkRevisions(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "current", "Current"))
	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "stale", "Stale"))
	if err != nil || got.SelectedNetworkBinding.InterfaceID != "current" || len(factory.creationInputs()) != 1 {
		t.Fatalf("stale network revision changed session: %#v; calls = %d; err = %v", got, len(factory.creationInputs()), err)
	}
}

func TestSessionNetworkLossCancelsWithTerminateWithoutLogout(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	defer run.unblock(nil)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil))
	cause := run.waitForCancellation(ctx)
	cancellation, ok := cause.(protocol.AuthenticationProtocolRunCancellationCause)
	if !ok || cancellation.CleanupRequirement != protocol.TerminateWithoutLogout {
		t.Fatal("network loss did not request terminate without logout")
	}
}

func TestExplicitSessionKeepsEstablishedRunWhenNonselectedPrefixChanges(t *testing.T) {
	ctx := testContext(t)
	factory := &controlledFactory{holdCancellation: true}
	address := netip.MustParseAddr("127.0.0.1")
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	definition.Configuration.NetworkBindingPolicy = NetworkBindingPolicy{
		Mode: ExplicitInterfaceAndLocalIPv4, InterfaceID: "lo", LocalIPv4Address: address,
	}
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	defer shutdownTestSession(t, session)

	snapshotWithPrefixes := func(revision uint64, prefixes ...uint8) environment.Snapshot {
		assignments := make([]environment.IPv4AddressAssignment, 0, len(prefixes))
		for _, prefix := range prefixes {
			assignments = append(assignments, environment.IPv4AddressAssignment{Address: address, PrefixLength: prefix})
		}
		iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
			InterfaceID: "lo", OperationalState: environment.OperationalStateUp,
			EndpointInterface: true, IPv4AddressAssignments: assignments,
		})
		if err != nil {
			t.Fatalf("NewNetworkInterface() error = %v", err)
		}
		return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{iface})
	}
	apply := func(revision uint64, prefixes ...uint8) Snapshot {
		t.Helper()
		got, err := session.ApplySystemNetworkSnapshot(ctx, snapshotWithPrefixes(revision, prefixes...))
		if err != nil {
			t.Fatalf("ApplySystemNetworkSnapshot(%d) error = %v", revision, err)
		}
		return got
	}

	initial := apply(1, 8, 32)
	if initial.State != Authenticating {
		t.Fatalf("initial state = %v, want %v", initial.State, Authenticating)
	}
	run := waitForFactoryRun(t, ctx, factory, 0)
	defer run.unblock(nil)
	if selected := factory.creationInputs()[0].SelectedSystemNetworkBinding.LocalIPv4AddressAssignment(); selected.PrefixLength != 8 {
		t.Fatalf("initial run prefix = %d, want 8", selected.PrefixLength)
	}
	if err := run.establish(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, ctx, session, Authenticated)

	for _, update := range []struct {
		revision uint64
		prefixes []uint8
	}{
		{revision: 2, prefixes: []uint8{8}},
		{revision: 3, prefixes: []uint8{32, 8}},
	} {
		got := apply(update.revision, update.prefixes...)
		if got.State != Authenticated {
			t.Fatalf("state at revision %d = %v, want existing authenticated Run", update.revision, got.State)
		}
		if count := len(factory.creationInputs()); count != 1 {
			t.Fatalf("factory creation count at revision %d = %d, want 1", update.revision, count)
		}
	}
}

func TestExplicitSessionUnavailableNeverRunsAndLossWaits(t *testing.T) {
	for _, reconnect := range []bool{true, false} {
		t.Run(fmt.Sprint(reconnect), func(t *testing.T) {
			ctx := testContext(t)
			factory := &controlledFactory{holdCancellation: true}
			definition := validRuntimeDefinition(t)
			definition.AuthenticationProtocolFactory = factory
			definition.AutoReconnect = reconnect
			definition.Configuration.NetworkBindingPolicy = NetworkBindingPolicy{Mode: ExplicitInterfaceAndLocalIPv4, InterfaceID: "target", LocalIPv4Address: netip.MustParseAddr("192.0.2.10")}
			s, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
			if err != nil {
				t.Fatal(err)
			}
			s.Start()
			defer shutdownTestSession(t, s)
			check := func(got Snapshot) {
				t.Helper()
				if got.State != WaitingForNetwork || got.SelectedNetworkBinding != nil || got.StateReason == nil || got.StateReason.Code != StateReasonCodeNetworkBindingUnavailable || got.StateReason.Description != "The selected network binding is unavailable." {
					t.Fatal("unavailable binding state differs")
				}
			}
			check(sessionSnapshot(t, ctx, s))
			got, err := s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "other", "Other"))
			if err != nil {
				t.Fatal(err)
			}
			check(got)
			if len(factory.creationInputs()) != 0 {
				t.Fatal("unavailable target started Run")
			}
			_, err = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "target", "Target"))
			if err != nil {
				t.Fatal(err)
			}
			run := waitForFactoryRun(t, ctx, factory, 0)
			defer run.unblock(nil)
			if err = run.establish(ctx); err != nil {
				t.Fatal(err)
			}
			waitForState(t, ctx, s, Authenticated)
			got, err = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 3, "other", "Other"))
			if err != nil {
				t.Fatal(err)
			}
			check(got)
			assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
			_, err = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 4, "target", "Target"))
			if err != nil {
				t.Fatal(err)
			}
			if len(factory.creationInputs()) != 1 {
				t.Fatal("replacement Run overlapped cleanup")
			}
			run.unblock(nil)
			if reconnect {
				next := waitForFactoryRun(t, ctx, factory, 1)
				next.unblock(nil)
			} else {
				got = waitForState(t, ctx, s, BlockedByError)
				if got.StateReason == nil || got.StateReason.Code != StateReasonCodeAutomaticReconnectDisabled {
					t.Fatal("automatic reconnect policy lost")
				}
			}
		})
	}
}
