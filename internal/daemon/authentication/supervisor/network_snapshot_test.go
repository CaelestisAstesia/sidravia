package supervisor

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/session"
	environment "sidravia/internal/daemon/environment"
)

// testNetworkSnapshotWithInterface builds an environment.Snapshot containing
// one operational wired interface with a valid IPv4 assignment. It uses only
// the public environment constructors so the supervisor package does not
// depend on session-package test helpers.
func testNetworkSnapshotWithInterface(
	revision uint64,
	interfaceID environment.InterfaceID,
	address string,
) environment.Snapshot {
	iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:              interfaceID,
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address:      netip.MustParseAddr(address),
			PrefixLength: 24,
		}},
	})
	if err != nil {
		panic(err)
	}
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{iface})
}

func testNetworkSnapshot(revision uint64) environment.Snapshot {
	return testNetworkSnapshotWithInterface(revision, "iface-1", "192.0.2.10")
}

// TestSupervisorApplyNetworkSnapshotMovesWaitingSessionToBinding proves Task 6
// behavior 1: a Session started before the first network snapshot moves from
// waiting_for_network to a Snapshot with a selected binding after Apply.
func TestSupervisorApplyNetworkSnapshotMovesWaitingSessionToBinding(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	id, initial, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}
	if initial.State != session.WaitingForNetwork {
		t.Fatalf("initial state = %q, want waiting_for_network", initial.State)
	}
	if initial.SelectedNetworkBinding != nil {
		t.Fatalf("expected no binding initially, got %+v", initial.SelectedNetworkBinding)
	}

	if err := supervisor.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshot(1)); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot error: %v", err)
	}

	snapshot, err := supervisor.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if snapshot.State != session.Authenticating {
		t.Errorf("state after Apply = %q, want authenticating", snapshot.State)
	}
	if snapshot.SelectedNetworkBinding == nil {
		t.Fatal("expected selected network binding after Apply")
	}
	if snapshot.SelectedNetworkBinding.InterfaceID != "iface-1" {
		t.Errorf("selected InterfaceID = %q, want iface-1", snapshot.SelectedNetworkBinding.InterfaceID)
	}
}

// TestSupervisorApplyNetworkSnapshotBeforeStartSelectsBinding proves Task 6
// behavior 2: a snapshot applied before Start is delivered before the returned
// initial Snapshot, so the new Session already has its selected binding.
func TestSupervisorApplyNetworkSnapshotBeforeStartSelectsBinding(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshot(1)); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot error: %v", err)
	}

	id, initial, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}
	if initial.SelectedNetworkBinding == nil {
		t.Fatal("expected selected binding in initial snapshot")
	}
	if initial.SelectedNetworkBinding.InterfaceID != "iface-1" {
		t.Errorf("initial binding InterfaceID = %q, want iface-1", initial.SelectedNetworkBinding.InterfaceID)
	}
	if initial.State != session.Authenticating {
		t.Errorf("initial state = %q, want authenticating", initial.State)
	}
	_ = id
}

// TestSupervisorNetworkSnapshotNewerReplacesOlderIgnored proves Task 6 behavior
// 3: a newer revision replaces the latest value, and an older revision cannot
// make a Session revert to a previous binding.
func TestSupervisorNetworkSnapshotNewerReplacesOlderIgnored(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	id, initial, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}
	if initial.SelectedNetworkBinding != nil {
		t.Fatalf("expected no binding initially, got %+v", initial.SelectedNetworkBinding)
	}

	first := testNetworkSnapshotWithInterface(1, "iface-a", "192.0.2.10")
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, first); err != nil {
		t.Fatalf("Apply rev 1 error: %v", err)
	}
	snap, err := supervisor.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after rev 1 error: %v", err)
	}
	if snap.SelectedNetworkBinding == nil || snap.SelectedNetworkBinding.InterfaceID != "iface-a" {
		t.Fatalf("after rev 1: binding = %+v, want iface-a", snap.SelectedNetworkBinding)
	}

	second := testNetworkSnapshotWithInterface(2, "iface-b", "198.51.100.10")
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, second); err != nil {
		t.Fatalf("Apply rev 2 error: %v", err)
	}
	snap, err = supervisor.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after rev 2 error: %v", err)
	}
	if snap.SelectedNetworkBinding == nil || snap.SelectedNetworkBinding.InterfaceID != "iface-b" {
		t.Fatalf("after rev 2: binding = %+v, want iface-b", snap.SelectedNetworkBinding)
	}

	// An older revision is ignored by the Supervisor and must not revert the
	// Session's binding.
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, first); err != nil {
		t.Fatalf("Apply older rev 1 error: %v", err)
	}
	snap, err = supervisor.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after older rev 1 error: %v", err)
	}
	if snap.SelectedNetworkBinding == nil || snap.SelectedNetworkBinding.InterfaceID != "iface-b" {
		t.Fatalf("after older rev 1: binding = %+v, want still iface-b", snap.SelectedNetworkBinding)
	}
}

// TestSupervisorNetworkSnapshotEqualRevisionReplaysStored proves Task 6
// behavior 4: an equal-revision replay uses the stored authoritative snapshot,
// not the caller's possibly different same-revision value. A new Session
// started after the replay receives the original stored binding.
func TestSupervisorNetworkSnapshotEqualRevisionReplaysStored(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	authoritative := testNetworkSnapshotWithInterface(1, "iface-a", "192.0.2.10")
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, authoritative); err != nil {
		t.Fatalf("Apply authoritative error: %v", err)
	}

	// Caller attempts to replace the same revision with different content.
	different := testNetworkSnapshotWithInterface(1, "iface-b", "198.51.100.10")
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, different); err != nil {
		t.Fatalf("Apply equal-revision error: %v", err)
	}

	// A new Session must receive the stored authoritative snapshot (iface-a),
	// not the caller's same-revision different value.
	_, initial, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}
	if initial.SelectedNetworkBinding == nil {
		t.Fatal("expected selected binding in initial snapshot")
	}
	if initial.SelectedNetworkBinding.InterfaceID != "iface-a" {
		t.Errorf("initial binding InterfaceID = %q, want iface-a (stored authoritative)", initial.SelectedNetworkBinding.InterfaceID)
	}
}

// TestSupervisorApplyNetworkSnapshotRejectsInvalidContextAndClosed proves Task 6
// behavior 5: nil and already-cancelled contexts return an error without
// changing the stored latest snapshot, and a closed Supervisor returns an
// error.
func TestSupervisorApplyNetworkSnapshotRejectsInvalidContextAndClosed(t *testing.T) {
	snapshot := testNetworkSnapshot(1)

	t.Run("nil context", func(t *testing.T) {
		supervisor := New(testSupervisorDeps())
		defer func() {
			_ = supervisor.Close()
			supervisor.Wait()
		}()

		if err := supervisor.ApplySystemNetworkSnapshot(nil, snapshot); err == nil {
			t.Fatal("expected error for nil context, got nil")
		}

		// A rejected context must not have stored a snapshot: a new Session
		// starts without a selected binding.
		_, initial, err := supervisor.StartResolved(context.Background(), testRuntimeDefinition(), session.SuspendAuthentication)
		if err != nil {
			t.Fatalf("StartResolved error: %v", err)
		}
		if initial.SelectedNetworkBinding != nil {
			t.Errorf("expected no binding after rejected apply, got %+v", initial.SelectedNetworkBinding)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		supervisor := New(testSupervisorDeps())
		defer func() {
			_ = supervisor.Close()
			supervisor.Wait()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := supervisor.ApplySystemNetworkSnapshot(ctx, snapshot); err == nil {
			t.Fatal("expected error for cancelled context, got nil")
		}

		_, initial, err := supervisor.StartResolved(context.Background(), testRuntimeDefinition(), session.SuspendAuthentication)
		if err != nil {
			t.Fatalf("StartResolved error: %v", err)
		}
		if initial.SelectedNetworkBinding != nil {
			t.Errorf("expected no binding after rejected apply, got %+v", initial.SelectedNetworkBinding)
		}
	})

	t.Run("closed supervisor", func(t *testing.T) {
		supervisor := New(testSupervisorDeps())
		_ = supervisor.Close()
		supervisor.Wait()

		if err := supervisor.ApplySystemNetworkSnapshot(context.Background(), snapshot); err == nil {
			t.Fatal("expected error for closed supervisor, got nil")
		}
	})
}

// TestSupervisorFailedStartAfterSnapshotLeavesNoResidual proves Task 6 behavior
// 6: after a latest snapshot has been applied, a failed Start (latest-snapshot
// application fails because of a pre-cancelled context) leaves no residual
// Session, and a later valid Start remains possible and receives the stored
// snapshot.
func TestSupervisorFailedStartAfterSnapshotLeavesNoResidual(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshot(1)); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot error: %v", err)
	}

	// Start with a pre-cancelled context: the latest-snapshot application to
	// the new Session fails and the new Session is fully rolled back.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := supervisor.StartResolved(cancelledCtx, testRuntimeDefinition(), session.MaintainAuthentication); err == nil {
		t.Fatal("expected error from cancelled-context Start after snapshot, got nil")
	}

	// No residual Session remains.
	snapshots, err := supervisor.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("expected no residual sessions after failed start, got %d", len(snapshots))
	}

	// A later valid Start still works and receives the stored snapshot.
	id, initial, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("subsequent StartResolved error: %v", err)
	}
	if initial.SelectedNetworkBinding == nil {
		t.Error("expected selected binding in initial snapshot from stored latest")
	}
	if initial.SelectedNetworkBinding != nil && initial.SelectedNetworkBinding.InterfaceID != "iface-1" {
		t.Errorf("initial binding InterfaceID = %q, want iface-1", initial.SelectedNetworkBinding.InterfaceID)
	}
	_ = id
}

func TestLatestSystemNetworkSnapshotAuthorityAndIsolation(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer func() { _ = sup.Close(); sup.Wait() }()
	ctx := context.Background()
	if got, available, err := sup.LatestSystemNetworkSnapshot(ctx); err != nil || available || got.Revision != 0 {
		t.Fatalf("initial read = %v %t %v", got, available, err)
	}
	if err := sup.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshotWithInterface(5, "accepted", "192.0.2.5")); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []environment.Snapshot{testNetworkSnapshotWithInterface(5, "equal-rejected", "192.0.2.6"), testNetworkSnapshotWithInterface(4, "old-rejected", "192.0.2.4")} {
		if err := sup.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	got, available, err := sup.LatestSystemNetworkSnapshot(ctx)
	if err != nil || !available || got.Revision != 5 || got.ObservedAt != time.Unix(5, 0) || got.Interfaces()[0].InterfaceID != "accepted" {
		t.Fatalf("authority = %v %t %v", got, available, err)
	}
	rows := got.Interfaces()
	rows[0].InterfaceID = "mutated"
	assignments := rows[0].IPv4AddressAssignments()
	assignments[0].Address = netip.MustParseAddr("192.0.2.99")
	got.Revision = 99
	got.ObservedAt = time.Time{}
	again, _, err := sup.LatestSystemNetworkSnapshot(ctx)
	if err != nil || again.Revision != 5 || again.Interfaces()[0].InterfaceID != "accepted" || again.Interfaces()[0].IPv4AddressAssignments()[0].Address.String() != "192.0.2.5" {
		t.Fatal("caller mutated accepted facts")
	}
	if _, _, err := sup.LatestSystemNetworkSnapshot(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := sup.LatestSystemNetworkSnapshot(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel cause = %v", err)
	}
	_ = sup.Close()
	sup.Wait()
	if _, _, err := sup.LatestSystemNetworkSnapshot(ctx); err == nil {
		t.Fatal("closed Supervisor returned facts")
	}
}

func TestAcceptedNetworkEventsWithoutSessionsAndEmptyInterfaces(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer sup.Close()
	stream := subscribeForTest(t, sup)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, snapshot := range []environment.Snapshot{environment.NewSnapshot(0, time.Unix(10, 0), nil), testNetworkSnapshot(2), environment.NewSnapshot(3, time.Unix(30, 0), nil)} {
		if err := sup.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
		event := nextForTest(t, ctx, stream)
		if event.Kind != StateNetworkChanged || event.SessionID != "" || event.Revision != 0 || event.Snapshot != (Snapshot{}) || event.NetworkSnapshot.Revision != snapshot.Revision || event.NetworkSnapshot.ObservedAt != snapshot.ObservedAt || len(event.NetworkSnapshot.Interfaces()) != len(snapshot.Interfaces()) {
			t.Fatal("accepted network event incomplete", event)
		}
	}
	for _, snapshot := range []environment.Snapshot{testNetworkSnapshot(2), testNetworkSnapshot(3)} {
		if err := sup.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	quiet, quietCancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer quietCancel()
	if event, err := stream.Next(quiet); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("old/equal emitted duplicate", event, err)
	}
}

func TestEqualNetworkReplayFeedsActorsWithoutDuplicateNetworkEvent(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer sup.Close()
	stream := subscribeForTest(t, sup)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sup.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshotWithInterface(4, "accepted", "192.0.2.4")); err != nil {
		t.Fatal(err)
	}
	if event := nextForTest(t, ctx, stream); event.Kind != StateNetworkChanged {
		t.Fatal(event)
	}
	definition := testRuntimeDefinition()
	definition.Configuration.AuthenticationSessionID = "replay"
	actor, err := session.NewAuthenticationSession(definition, session.SuspendAuthentication, sup.deps)
	if err != nil {
		t.Fatal(err)
	}
	actor.Start()
	managed := installTestForwarder(sup, "replay", actor)
	if err := sup.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshotWithInterface(4, "rejected", "192.0.2.44")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := actor.Snapshot(ctx)
	if err != nil || snapshot.SelectedNetworkBinding == nil || snapshot.SelectedNetworkBinding.InterfaceID != "accepted" {
		t.Fatal("equal replay lost stored facts", snapshot, err)
	}
	if err := sup.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshotWithInterface(5, "new", "192.0.2.5")); err != nil {
		t.Fatal(err)
	}
	snapshot, err = actor.Snapshot(ctx)
	if err != nil || snapshot.SelectedNetworkBinding == nil || snapshot.SelectedNetworkBinding.InterfaceID != "new" {
		t.Fatal("new revision did not feed current actor", snapshot, err)
	}
	// Shutdown drains all actor events, so this read cannot miss a late duplicate.
	if err := actor.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-managed.forwardDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	networkCount := 0
	quiet, quietCancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer quietCancel()
	for {
		event, err := stream.Next(quiet)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			break
		}
		if event.Kind == StateNetworkChanged {
			networkCount++
			if event.NetworkSnapshot.Revision != 5 || event.NetworkSnapshot.Interfaces()[0].InterfaceID != "new" {
				t.Fatal(event)
			}
		}
	}
	if networkCount != 1 {
		t.Fatal("equal replay duplicated network event", networkCount)
	}
}
