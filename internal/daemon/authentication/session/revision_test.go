package session

import (
	"context"
	"reflect"
	"testing"
	"time"

	"sidravia/internal/connection/auth/protocol"
	environment "sidravia/internal/connection/environment"
)

func TestSessionSnapshotsAndRuntimeDefinitionAreCloned(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, SuspendAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	definition.Configuration.ProtocolContextOverride[0] = '!'
	_, _ = session.Activate(ctx)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if got := string(factory.creationInputs()[0].ProtocolContextOverride); got != `{"network":"campus"}` {
		t.Fatalf("factory received aliased override %q", got)
	}
	first := sessionSnapshot(t, ctx, session)
	first.SelectedNetworkBinding.DisplayName = "mutated"
	second := sessionSnapshot(t, ctx, session)
	if second.SelectedNetworkBinding.DisplayName != "Ethernet" {
		t.Fatalf("snapshot is aliased: %#v", second)
	}
}

func TestSessionRevisionEventsCoalesce(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	select {
	case <-session.RevisionEvents():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Campus Ethernet"))
	final := sessionSnapshot(t, ctx, session)
	select {
	case event := <-session.RevisionEvents():
		if event.Revision != final.Revision || event.AuthenticationSessionID != final.AuthenticationSessionID || !reflect.DeepEqual(event.Snapshot, final) {
			t.Fatalf("coalesced revision event = %#v, final snapshot = %#v", event, final)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestSessionSnapshotRevisionIsMonotonicAndNoOpsDoNotIncrement(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	initial := sessionSnapshot(t, ctx, session)
	activated, err := session.Activate(ctx)
	if err != nil {
		t.Fatalf("activate() error = %v", err)
	}
	assertSameRevision(t, "no-op activate without network", activated, initial)

	network := usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet")
	authenticating, err := session.ApplySystemNetworkSnapshot(ctx, network)
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	run := factory.run(0)
	defer run.unblock(nil)
	assertRevisionIncreased(t, "initial binding and successful run", authenticating, initial)

	equivalent, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Ethernet"))
	if err != nil {
		t.Fatalf("equivalent applySystemNetworkSnapshot() error = %v", err)
	}
	assertSameRevision(t, "equivalent higher network revision", equivalent, authenticating)

	stale, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "stale", "Stale"))
	if err != nil {
		t.Fatalf("stale applySystemNetworkSnapshot() error = %v", err)
	}
	assertSameRevision(t, "stale network revision", stale, equivalent)

	if err := run.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	authenticated := waitForState(t, ctx, session, Authenticated)
	assertNextRevision(t, "authentication establishment", authenticated, stale)
	session.post(authenticationEstablishedEvent{generation: 1})
	duplicate := sessionSnapshot(t, ctx, session)
	assertSameRevision(t, "duplicate establishment", duplicate, authenticated)

	displayUpdate, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 3, "ethernet", "Campus Ethernet"))
	if err != nil {
		t.Fatalf("display applySystemNetworkSnapshot() error = %v", err)
	}
	assertNextRevision(t, "public display-name update", displayUpdate, duplicate)
	activated, err = session.Activate(ctx)
	if err != nil {
		t.Fatalf("healthy activate() error = %v", err)
	}
	assertSameRevision(t, "healthy activate", activated, displayUpdate)

	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	if stopping.State != Stopping {
		t.Fatalf("suspend state = %q, want %q", stopping.State, Stopping)
	}
	assertNextRevision(t, "begin suspend", stopping, activated)
	repeatedSuspend, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("repeated suspend() error = %v", err)
	}
	assertSameRevision(t, "repeated suspend", repeatedSuspend, stopping)
	run.unblock(nil)
	suspended := waitForState(t, ctx, session, Suspended)
	assertNextRevision(t, "complete suspend", suspended, stopping)
}

func TestStoppingCannotBeOverwrittenByInputsOrCallbacks(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, withStandby(t, usableSystemNetworkSnapshot(t, 1, "first", "First"), "second"))
	run := factory.run(0)
	defer run.unblock(nil)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}
	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)

	if got, err := session.Activate(ctx); err != nil || got.State != Stopping || got.Intent != SuspendAuthentication {
		t.Fatalf("activate while stopping = (%#v, %v)", got, err)
	}
	if got, err := session.Restart(ctx); err != nil || got.State != Stopping || got.Intent != SuspendAuthentication {
		t.Fatalf("restart while stopping = (%#v, %v)", got, err)
	}
	if got, err := session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil)); err != nil || got.State != Stopping {
		t.Fatalf("network loss while stopping = (%#v, %v)", got, err)
	}
	session.post(authenticationEstablishedEvent{generation: 1})
	if got := sessionSnapshot(t, ctx, session); got.State != Stopping || got.Revision < stopping.Revision {
		t.Fatalf("callback overwrote stopping: %#v", got)
	}

	run.unblock(nil)
	_ = waitForState(t, ctx, session, Suspended)
}

func assertNextRevision(t *testing.T, operation string, got, previous Snapshot) {
	t.Helper()
	if got.Revision != previous.Revision+1 {
		t.Fatalf("%s revision = %d, want %d after revision %d", operation, got.Revision, previous.Revision+1, previous.Revision)
	}
}

func assertSameRevision(t *testing.T, operation string, got, previous Snapshot) {
	t.Helper()
	if got.Revision != previous.Revision {
		t.Fatalf("%s revision = %d, want unchanged %d", operation, got.Revision, previous.Revision)
	}
}

func assertRevisionIncreased(t *testing.T, operation string, got, previous Snapshot) {
	t.Helper()
	if got.Revision <= previous.Revision {
		t.Fatalf("%s revision = %d, want greater than %d", operation, got.Revision, previous.Revision)
	}
}

func assertRevisionEvent(t *testing.T, ctx context.Context, revisions <-chan RevisionEvent, snapshot Snapshot) {
	t.Helper()
	select {
	case event, ok := <-revisions:
		if !ok || event.AuthenticationSessionID != snapshot.AuthenticationSessionID || event.Revision != snapshot.Revision || !reflect.DeepEqual(event.Snapshot, snapshot) {
			t.Fatalf("revision event = %#v, want session %q revision %d", event, snapshot.AuthenticationSessionID, snapshot.Revision)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func drainRevisionEvents(revisions <-chan RevisionEvent) {
	for {
		select {
		case _, ok := <-revisions:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func TestSessionRevisionEventRetainsInitialPublicationAndOwnsClone(t *testing.T) {
	factory := &controlledFactory{}
	actor := newTestSession(t, factory, MaintainAuthentication)
	defer shutdownTestSession(t, actor)
	ctx := testContext(t)
	initial := sessionSnapshot(t, ctx, actor)
	var event RevisionEvent
	select {
	case event = <-actor.RevisionEvents():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if event.Revision != initial.Revision || event.AuthenticationSessionID != initial.AuthenticationSessionID || !reflect.DeepEqual(event.Snapshot, initial) {
		t.Fatalf("initial paired event = %#v", event)
	}
	event.Snapshot.StateReason.Description = "receiver mutation"
	if !reflect.DeepEqual(sessionSnapshot(t, ctx, actor), initial) {
		t.Fatal("initial receiver changed actor")
	}
	updated, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err != nil {
		t.Fatal(err)
	}
	assertRevisionEvent(t, ctx, actor.RevisionEvents(), updated)
	if event.Snapshot.Revision != initial.Revision || event.Snapshot.SelectedNetworkBinding != nil || event.Snapshot.ProtocolSocket.RunGeneration != 0 {
		t.Fatal("older event acquired later actor contents")
	}
}
