package session

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func queryDiagnostics(t *testing.T, ctx context.Context, actor *AuthenticationSession) NetworkDiagnosticsSnapshot {
	t.Helper()
	got, err := actor.QueryNetworkDiagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func runNetworkObserver(t *testing.T, ctx context.Context, run *controlledRun) protocol.AuthenticationProtocolRunNetworkObserver {
	t.Helper()
	select {
	case observer := <-run.started:
		network, ok := observer.(protocol.AuthenticationProtocolRunNetworkObserver)
		if !ok {
			t.Fatal("Session observer lacks network capability")
		}
		return network
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}

func TestNetworkDiagnosticsQueryIsClonedReadOnlyActorTuple(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	actor := newTestSession(t, factory, MaintainAuthentication)
	defer shutdownTestSession(t, actor)
	ctx := testContext(t)
	initial := queryDiagnostics(t, ctx, actor)
	if initial.ProtocolSocket != (ProtocolSocketObservation{State: ProtocolSocketNotObserved}) || initial.Snapshot.ProtocolSocket != initial.ProtocolSocket {
		t.Fatalf("initial = %#v", initial)
	}
	initial.Snapshot.StateReason.Description = "caller mutation"
	if queryDiagnostics(t, ctx, actor).Snapshot.StateReason.Description == "caller mutation" || len(factory.creationInputs()) != 0 {
		t.Fatal("query mutated actor or created Run")
	}
	_, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First"))
	if err != nil {
		t.Fatal(err)
	}
	run := factory.run(0)
	defer run.unblock(nil)
	observer := runNetworkObserver(t, ctx, run)
	before := queryDiagnostics(t, ctx, actor)
	if before.ProtocolSocket.State != ProtocolSocketNotObserved || before.ProtocolSocket.RunGeneration != 1 {
		t.Fatalf("before opening = %#v", before)
	}
	// A close without an accepted opening must not invent socket facts.
	observer.ProtocolSocketClosed(true)
	withoutOpen := queryDiagnostics(t, ctx, actor)
	if !reflect.DeepEqual(withoutOpen, before) {
		t.Fatal("close without open changed actor tuple or revision")
	}
	local, remote := netip.MustParseAddrPort("127.0.0.1:40001"), netip.MustParseAddrPort("255.255.255.255:61440")
	if err := observer.ProtocolSocketOpened(local, remote); err != nil {
		t.Fatal(err)
	}
	opened := queryDiagnostics(t, ctx, actor)
	if opened.ProtocolSocket != (ProtocolSocketObservation{RunGeneration: 1, State: ProtocolSocketOpen, LocalEndpoint: local, RemoteEndpoint: remote, UpdatedAt: time.Unix(100, 0)}) {
		t.Fatalf("opened = %#v", opened.ProtocolSocket)
	}
	if opened.Snapshot.ProtocolSocket != opened.ProtocolSocket {
		t.Fatal("diagnostic socket differs from actor Snapshot")
	}
	assertNextRevision(t, "accepted socket opening", opened.Snapshot, before.Snapshot)
	assertRevisionEvent(t, ctx, actor.RevisionEvents(), opened.Snapshot)
	if err := observer.ProtocolSocketOpened(netip.MustParseAddrPort("127.0.0.2:40002"), remote); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(queryDiagnostics(t, ctx, actor), opened) {
		t.Fatal("duplicate opening replaced actor tuple or revision")
	}
	opened.Snapshot.SelectedNetworkBinding.InterfaceID = "caller mutation"
	got := queryDiagnostics(t, ctx, actor)
	if got.Snapshot.SelectedNetworkBinding.InterfaceID != "first" || len(factory.creationInputs()) != 1 {
		t.Fatal("diagnostics query mutated actor or created Run")
	}
	// Canceled active contexts still accept actual closure before finished.
	stopping, err := actor.Suspend(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cause := run.waitForCancellation(ctx); cause == nil {
		t.Fatal("Run not canceled")
	}
	observer.ProtocolSocketClosed(true)
	closed := queryDiagnostics(t, ctx, actor)
	if closed.ProtocolSocket.State != ProtocolSocketClosed || closed.ProtocolSocket.LocalEndpoint != local || closed.Snapshot.ProtocolSocket != closed.ProtocolSocket {
		t.Fatalf("closed = %#v", closed)
	}
	assertNextRevision(t, "accepted canceled socket closure", closed.Snapshot, stopping)
	assertRevisionEvent(t, ctx, actor.RevisionEvents(), closed.Snapshot)
	run.unblock(nil)
	waitForState(t, ctx, actor, Suspended)
	if queryDiagnostics(t, ctx, actor).ProtocolSocket != closed.ProtocolSocket {
		t.Fatal("finished relabeled closed socket")
	}
}

func TestNetworkDiagnosticsRejectsImpossibleEndpointsSafely(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	actor := newTestSession(t, factory, MaintainAuthentication)
	defer shutdownTestSession(t, actor)
	ctx := testContext(t)
	if _, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First")); err != nil {
		t.Fatal(err)
	}
	run := factory.run(0)
	defer run.unblock(nil)
	observer := runNetworkObserver(t, ctx, run)
	valid := netip.MustParseAddrPort("127.0.0.1:12345")
	invalid := []netip.AddrPort{{}, netip.MustParseAddrPort("0.0.0.0:12345"), netip.MustParseAddrPort("127.0.0.1:0"), netip.MustParseAddrPort("[::1]:12345"), netip.MustParseAddrPort("[::ffff:127.0.0.1]:12345")}
	for _, endpoint := range invalid {
		for _, pair := range [][2]netip.AddrPort{{endpoint, valid}, {valid, endpoint}} {
			err := observer.ProtocolSocketOpened(pair[0], pair[1])
			if err == nil || err.Error() != "authentication protocol socket endpoints are invalid" {
				t.Fatalf("unsafe contract result = %v", err)
			}
		}
	}
	if queryDiagnostics(t, ctx, actor).ProtocolSocket.State != ProtocolSocketNotObserved {
		t.Fatal("invalid callback posted socket facts")
	}
}

func TestNetworkDiagnosticsGenerationIgnoresStaleAndRetainsFailedClose(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	actor := newTestSession(t, factory, MaintainAuthentication)
	defer shutdownTestSession(t, actor)
	ctx := testContext(t)
	if _, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First")); err != nil {
		t.Fatal(err)
	}
	first := factory.run(0)
	defer first.unblock(nil)
	oldObserver := runNetworkObserver(t, ctx, first)
	local, remote := netip.MustParseAddrPort("127.0.0.1:40001"), netip.MustParseAddrPort("127.0.0.1:61440")
	if err := oldObserver.ProtocolSocketOpened(local, remote); err != nil {
		t.Fatal(err)
	}
	if _, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second")); err != nil {
		t.Fatal(err)
	}
	if first.waitForCancellation(ctx) == nil {
		t.Fatal("first Run not canceled")
	}
	oldObserver.ProtocolSocketClosed(true)
	first.unblock(nil)
	second := waitForFactoryRun(t, ctx, factory, 1)
	defer second.unblock(nil)
	newObserver := runNetworkObserver(t, ctx, second)
	fresh := queryDiagnostics(t, ctx, actor)
	if fresh.ProtocolSocket.RunGeneration != 2 || fresh.ProtocolSocket.State != ProtocolSocketNotObserved || fresh.ProtocolSocket.LocalEndpoint.IsValid() {
		t.Fatalf("fresh = %#v", fresh)
	}
	if err := oldObserver.ProtocolSocketOpened(local, remote); err != nil {
		t.Fatal(err)
	}
	oldObserver.ProtocolSocketClosed(false)
	if !reflect.DeepEqual(queryDiagnostics(t, ctx, actor), fresh) {
		t.Fatal("old generation changed actor tuple or revision")
	}
	local = netip.MustParseAddrPort("127.0.0.2:40002")
	if err := newObserver.ProtocolSocketOpened(local, remote); err != nil {
		t.Fatal(err)
	}
	stopping, err := actor.Suspend(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.waitForCancellation(ctx) == nil {
		t.Fatal("second Run not canceled")
	}
	newObserver.ProtocolSocketClosed(false)
	closeFailed := queryDiagnostics(t, ctx, actor)
	if closeFailed.ProtocolSocket.State != ProtocolSocketCloseFailed || closeFailed.Snapshot.ProtocolSocket != closeFailed.ProtocolSocket {
		t.Fatal("close_failed missing from full Snapshot")
	}
	assertNextRevision(t, "accepted failed closure", closeFailed.Snapshot, stopping)
	assertRevisionEvent(t, ctx, actor.RevisionEvents(), closeFailed.Snapshot)
	second.unblock(nil)
	waitForState(t, ctx, actor, Suspended)
	failed := queryDiagnostics(t, ctx, actor)
	if failed.ProtocolSocket.State != ProtocolSocketCloseFailed || failed.ProtocolSocket.RunGeneration != 2 || failed.ProtocolSocket.LocalEndpoint != local {
		t.Fatalf("failed close = %#v", failed)
	}
	newObserver.ProtocolSocketClosed(true)
	if !reflect.DeepEqual(queryDiagnostics(t, ctx, actor), failed) {
		t.Fatal("late callback changed actor tuple or revision")
	}
}

func TestNetworkDiagnosticsFailedFactoryRetainsPreviousGeneration(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	actor := newTestSession(t, factory, MaintainAuthentication)
	defer shutdownTestSession(t, actor)
	ctx := testContext(t)
	if _, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First")); err != nil {
		t.Fatal(err)
	}
	run := factory.run(0)
	defer run.unblock(nil)
	observer := runNetworkObserver(t, ctx, run)
	if err := observer.ProtocolSocketOpened(netip.MustParseAddrPort("127.0.0.1:40001"), netip.MustParseAddrPort("127.0.0.1:61440")); err != nil {
		t.Fatal(err)
	}
	if _, err := actor.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	if run.waitForCancellation(ctx) == nil {
		t.Fatal("Run not canceled")
	}
	observer.ProtocolSocketClosed(true)
	run.unblock(nil)
	waitForState(t, ctx, actor, Suspended)
	retained := queryDiagnostics(t, ctx, actor).ProtocolSocket
	if _, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second")); err != nil {
		t.Fatal(err)
	}
	factory.mu.Lock()
	factory.creationError = errControlledFactoryCreation
	factory.mu.Unlock()
	if _, err := actor.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	failed := queryDiagnostics(t, ctx, actor)
	if failed.Snapshot.State != BlockedByError || failed.Snapshot.SelectedNetworkBinding.InterfaceID != "second" || failed.ProtocolSocket != retained || failed.Snapshot.ProtocolSocket != retained {
		t.Fatalf("failed factory relabeled prior socket: %#v", failed)
	}
	if len(factory.creationInputs()) != 2 {
		t.Fatal("diagnostics created Run")
	}
}

func TestNetworkDiagnosticsMissingCloseIsUnconfirmed(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	capture := &captureDiagnostics{}
	actor := newDiagnosticsSession(t, factory, capture)
	defer shutdownTestSession(t, actor)
	ctx := testContext(t)
	if _, err := actor.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First")); err != nil {
		t.Fatal(err)
	}
	run := factory.run(0)
	defer run.unblock(nil)
	observer := runNetworkObserver(t, ctx, run)
	local, remote := netip.MustParseAddrPort("127.0.0.1:40001"), netip.MustParseAddrPort("127.0.0.1:61440")
	if err := observer.ProtocolSocketOpened(local, remote); err != nil {
		t.Fatal(err)
	}
	opened := queryDiagnostics(t, ctx, actor).Snapshot
	run.unblock(nil)
	waitForState(t, ctx, actor, BlockedByError)
	got := queryDiagnostics(t, ctx, actor)
	if got.ProtocolSocket.State != ProtocolSocketCloseUnconfirmed || got.ProtocolSocket.LocalEndpoint != local || got.ProtocolSocket.RemoteEndpoint != remote || got.Snapshot.ProtocolSocket != got.ProtocolSocket {
		t.Fatalf("unconfirmed = %#v", got)
	}
	if got.Snapshot.Revision != opened.Revision+2 {
		t.Fatal("missing closure and Run failure did not each commit one revision")
	}
	capture.mu.Lock()
	records := append([]Snapshot(nil), capture.snapshots...)
	capture.mu.Unlock()
	found := false
	for _, record := range records {
		if record.Revision == opened.Revision+1 {
			found = true
			if record.ProtocolSocket.State != ProtocolSocketCloseUnconfirmed || record.ProtocolSocket.LocalEndpoint != local || record.State != opened.State {
				t.Fatal("unconfirmed transition missing from full actor commit")
			}
		}
	}
	if !found {
		t.Fatal("no committed unconfirmed revision")
	}
	observer.ProtocolSocketClosed(true)
	if queryDiagnostics(t, ctx, actor).ProtocolSocket != got.ProtocolSocket {
		t.Fatal("late close relabeled completed Run")
	}
}

func TestNetworkDiagnosticsQueryContextAndShutdown(t *testing.T) {
	factory := &controlledFactory{}
	actor := newTestSession(t, factory, SuspendAuthentication)
	defer func() {
		if !actor.closed.Load() {
			shutdownTestSession(t, actor)
		}
	}()
	if _, err := actor.QueryNetworkDiagnostics(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	cause := errors.New("query caller canceled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	if _, err := actor.QueryNetworkDiagnostics(ctx); !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatalf("canceled cause = %v", err)
	}
	// A buffered reply remains actor-owned after the caller stops waiting.
	reply := make(chan networkDiagnosticsReply, 1)
	if err := actor.send(context.Background(), networkDiagnosticsQuery{reply: reply}); err != nil {
		t.Fatal(err)
	}
	queryDiagnostics(t, testContext(t), actor)
	if len(factory.creationInputs()) != 0 {
		t.Fatal("query started a Run")
	}
	if err := actor.Shutdown(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := actor.QueryNetworkDiagnostics(context.Background()); !errors.Is(err, ErrAuthenticationSessionClosed) {
		t.Fatalf("shutdown query = %v", err)
	}
}

type immutableTargetFactory struct {
	controlledFactory
	reads int
	cause error
}

func (f *immutableTargetFactory) NetworkDiagnosticEndpoint(raw protocol.InstitutionProtocolConfiguration) (netip.AddrPort, error) {
	f.reads++
	if len(raw) > 0 {
		raw[0] = '['
	}
	return netip.MustParseAddrPort("192.0.2.1:61440"), f.cause
}
func TestImmutableNetworkTargetDoesNotCreateRunOrAliasDefinition(t *testing.T) {
	factory := &immutableTargetFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	original := append([]byte(nil), definition.InstitutionProfile.InstitutionProtocolConfiguration...)
	actor, err := NewAuthenticationSession(definition, SuspendAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatal(err)
	}
	actor.Start()
	defer func() {
		if !actor.closed.Load() {
			shutdownTestSession(t, actor)
		}
	}()
	definition.InstitutionProfile.InstitutionProtocolConfiguration[0] = '!'
	got, err := actor.NetworkDiagnosticTarget(context.Background())
	if err != nil || !got.Supported || got.Endpoint.String() != "192.0.2.1:61440" || len(factory.creationInputs()) != 0 || !reflect.DeepEqual(actor.definition.InstitutionProfile.InstitutionProtocolConfiguration, protocol.InstitutionProtocolConfiguration(original)) {
		t.Fatal("immutable target mutated definition or created Run")
	}
	cause := errors.New("target-cause")
	factory.cause = cause
	if _, err := actor.NetworkDiagnosticTarget(context.Background()); !errors.Is(err, cause) {
		t.Fatal("provider cause lost")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	for _, ctx := range []context.Context{nil, ctx} {
		_, err := actor.NetworkDiagnosticTarget(ctx)
		if err == nil {
			t.Fatal("invalid context accepted")
		}
		if ctx != nil && !errors.Is(err, cause) {
			t.Fatal("cancellation cause lost")
		}
	}
	if err := actor.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := actor.NetworkDiagnosticTarget(context.Background()); !errors.Is(err, ErrAuthenticationSessionClosed) {
		t.Fatal("closed target available")
	}
	unsupported := newTestSession(t, &controlledFactory{}, SuspendAuthentication)
	defer shutdownTestSession(t, unsupported)
	target, err := unsupported.NetworkDiagnosticTarget(context.Background())
	if err != nil || target.Supported || target.Endpoint.IsValid() {
		t.Fatal("provider-less target invented")
	}
}
