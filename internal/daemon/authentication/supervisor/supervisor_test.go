package supervisor

import (
	"context"
	"errors"
	"testing"

	"sidravia/internal/daemon/authentication/session"
)

func TestMissingSessionPreservesStableErrorIdentity(t *testing.T) {
	supervisor := New(Dependencies{})
	_, err := supervisor.Get(context.Background(), "missing")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v", err)
	}
}

func TestSupervisorStartResolvedAllocatesSessionID(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	id1, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.SuspendAuthentication)
	if err != nil {
		t.Fatalf("StartResolved (1) error: %v", err)
	}
	if id1 != "session-1" {
		t.Errorf("first SessionID = %q, want %q", id1, "session-1")
	}

	id2, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.SuspendAuthentication)
	if err != nil {
		t.Fatalf("StartResolved (2) error: %v", err)
	}
	if id2 != "session-2" {
		t.Errorf("second SessionID = %q, want %q", id2, "session-2")
	}
}

func TestSupervisorGetReturnsLatestSnapshot(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	snapshot, err := supervisor.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if snapshot.AuthenticationSessionID != id {
		t.Errorf("snapshot AuthenticationSessionID = %q, want %q", snapshot.AuthenticationSessionID, id)
	}
}

func TestSupervisorListReturnsAllSessions(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	id1, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved (1) error: %v", err)
	}

	if _, err := supervisor.Stop(ctx, id1); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	waitForSupervisorState(t, supervisor, id1, session.Suspended)

	id2, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved (2) error: %v", err)
	}

	snapshots, err := supervisor.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("len(snapshots) = %d, want 2", len(snapshots))
	}
	if snapshots[0].AuthenticationSessionID != id1 ||
		snapshots[1].AuthenticationSessionID != id2 {
		t.Errorf("List order = [%q, %q], want [%q, %q]",
			snapshots[0].AuthenticationSessionID,
			snapshots[1].AuthenticationSessionID,
			id1,
			id2,
		)
	}
	if snapshots[0].State != session.Suspended {
		t.Errorf("first retained Session state = %q, want suspended", snapshots[0].State)
	}
}
