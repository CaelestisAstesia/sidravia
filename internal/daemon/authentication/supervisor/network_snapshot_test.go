package supervisor

import (
	"context"
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
