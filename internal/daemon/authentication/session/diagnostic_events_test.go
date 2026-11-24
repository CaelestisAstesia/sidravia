package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

// captureDiagnostics records session diagnostic events for assertion. It never
// alters Session behavior.
type captureDiagnostics struct {
	mu             sync.Mutex
	snapshots      []Snapshot
	commands       []string
	generations    []uint64
	retryScheduled []time.Time
}

func (d *captureDiagnostics) SessionSnapshot(snapshot Snapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshots = append(d.snapshots, snapshot)
}

func (d *captureDiagnostics) SessionCommand(command string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands = append(d.commands, command)
}

func (d *captureDiagnostics) ProtocolRunGeneration(generation uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.generations = append(d.generations, generation)
}

func (d *captureDiagnostics) RetryScheduled(nextRetryAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retryScheduled = append(d.retryScheduled, nextRetryAt)
}

func newDiagnosticsSession(t *testing.T, factory *controlledFactory, capture Diagnostics) *AuthenticationSession {
	t.Helper()
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	deps := Dependencies{
		Now:            func() time.Time { return time.Unix(100, 0) },
		RetryPolicy:    unavailableRetryPolicy{},
		RetryScheduler: noOpRetryScheduler{},
		Diagnostics:    capture,
	}
	authSession, err := NewAuthenticationSession(definition, MaintainAuthentication, deps)
	if err != nil {
		t.Fatalf("NewAuthenticationSession: %v", err)
	}
	authSession.Start()
	return authSession
}

// TestSessionDiagnosticsObservesEveryCommittedRevision proves SessionSnapshot is
// invoked once per committed revision without coalescing, so no intermediate
// public revision is lost.
func TestSessionDiagnosticsObservesEveryCommittedRevision(t *testing.T) {
	factory := &controlledFactory{}
	capture := &captureDiagnostics{}
	authSession := newDiagnosticsSession(t, factory, capture)
	defer func() { _ = authSession.Shutdown(context.Background()) }()

	ctx := testContext(t)
	// Wait for the run goroutine to start and publish the initial revision.
	initial, err := authSession.Snapshot(ctx)
	if err != nil {
		t.Fatalf("initial Snapshot: %v", err)
	}
	if _, err := authSession.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet")); err != nil {
		t.Fatal(err)
	}
	_ = waitForState(t, ctx, authSession, Authenticating)
	if err := waitForFactoryRun(t, ctx, factory, 0).establish(ctx); err != nil {
		t.Fatal(err)
	}
	_ = waitForState(t, ctx, authSession, Authenticated)
	if _, err := authSession.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	final := waitForState(t, ctx, authSession, Suspended)
	capture.mu.Lock()
	observed := append([]Snapshot(nil), capture.snapshots...)
	capture.mu.Unlock()
	if final.Revision-initial.Revision < 3 {
		t.Fatalf("insufficient committed transitions: %d -> %d", initial.Revision, final.Revision)
	}
	if uint64(len(observed)) != final.Revision-initial.Revision+1 {
		t.Fatalf("observed %d snapshots for revisions %d..%d", len(observed), initial.Revision, final.Revision)
	}
	for i, snapshot := range observed {
		if snapshot.Revision != initial.Revision+uint64(i) || snapshot.AuthenticationSessionID != final.AuthenticationSessionID {
			t.Fatalf("snapshot %d: identity/revision = %s/%d, want %s/%d", i, snapshot.AuthenticationSessionID, snapshot.Revision, final.AuthenticationSessionID, initial.Revision+uint64(i))
		}
	}
	if !publicSnapshotsEqual(observed[len(observed)-1], final) {
		t.Fatal("last diagnostic snapshot differs from final public state")
	}
}

// TestSessionDiagnosticsRecordsCommandAndGeneration proves SessionCommand and
// ProtocolRunGeneration are recorded without changing Session behavior.
func TestSessionDiagnosticsRecordsCommandAndGeneration(t *testing.T) {
	factory := &controlledFactory{}
	capture := &captureDiagnostics{}
	authSession := newDiagnosticsSession(t, factory, capture)
	defer func() { _ = authSession.Shutdown(context.Background()) }()

	ctx := testContext(t)
	_, _ = authSession.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	_ = waitForFactoryRun(t, ctx, factory, 0)

	capture.mu.Lock()
	commands := append([]string(nil), capture.commands...)
	generations := append([]uint64(nil), capture.generations...)
	capture.mu.Unlock()

	if len(generations) == 0 {
		t.Fatal("expected ProtocolRunGeneration to be called after run creation")
	}
	found := false
	for _, c := range commands {
		if c == "apply_network_snapshot" {
			found = true
		}
	}
	if !found {
		t.Errorf("apply_network_snapshot command not recorded; commands=%v", commands)
	}
}
