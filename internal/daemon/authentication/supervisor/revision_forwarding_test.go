package supervisor

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"sidravia/internal/connection/auth/protocol"
	"sidravia/internal/daemon/authentication/session"
)

func TestSupervisorRevisionEventsReceivesUpdates(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	events := subscribeForTest(t, supervisor)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	event := nextForTest(t, ctx, events)
	if event.SessionID != "session-1" {
		t.Errorf("event SessionID = %q, want session-1", event.SessionID)
	}
}

func TestSupervisorSlowSubscriberDoesNotBlock(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	_ = subscribeForTest(t, supervisor)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	for i := 0; i < 5; i++ {
		stopCtx, stopCancel := context.WithTimeout(ctx, time.Second)
		if _, err := supervisor.Stop(stopCtx, id); err != nil {
			stopCancel()
			t.Fatalf("Stop iteration %d error: %v", i, err)
		}
		stopCancel()
		waitForSupervisorState(t, supervisor, id, session.Suspended)

		restartCtx, restartCancel := context.WithTimeout(ctx, time.Second)
		if _, err := supervisor.Restart(restartCtx, id); err != nil {
			restartCancel()
			t.Fatalf("Restart iteration %d error: %v", i, err)
		}
		restartCancel()
	}
}

// TestSupervisorCloseRevisionChannelRace verifies that Close does not panic
// while ending an owned subscription concurrently with actor publications.
func TestSupervisorCloseRevisionChannelRace(t *testing.T) {
	for round := 0; round < 50; round++ {
		supervisor := New(testSupervisorDeps())

		ctx := context.Background()
		_, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
		if err != nil {
			supervisor.Close()
			supervisor.Wait()
			t.Fatalf("round %d: StartResolved error: %v", round, err)
		}

		events := subscribeForTest(t, supervisor)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if _, err := events.Next(context.Background()); err != nil {
					return
				}
			}
		}()

		if err := supervisor.Close(); err != nil {
			t.Fatalf("round %d: Close error: %v", round, err)
		}
		supervisor.Wait()
		wg.Wait()
	}
}

// TestSupervisorForwardExitsOnClosedRevisionChannel verifies that the forward
// goroutine exits promptly when the session's revision channel is closed,
// preventing a busy loop. It also verifies that Close+Wait does not hang.
func TestSupervisorForwardExitsOnClosedRevisionChannel(t *testing.T) {
	supervisor := New(testSupervisorDeps())

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	waitForSupervisorState(t, supervisor, id, session.Suspended)

	// ForgetStopped shuts down the producer and drains its closed revision
	// channel before returning.
	if err := supervisor.ForgetStopped(id); err != nil {
		t.Fatalf("ForgetStopped error: %v", err)
	}

	// Close and Wait must complete without hanging.
	done := make(chan struct{})
	go func() {
		_ = supervisor.Close()
		supervisor.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close+Wait timed out; forward goroutine may not have exited")
	}
} // TestSupervisorPreCancelledContextReturnsErrorAndCleansUp verifies that

type delayedForwardFactory struct {
	*stateFactory
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (factory *delayedForwardFactory) CreateAuthenticationProtocolRun(inputs protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	close(factory.entered)
	<-factory.release
	return factory.stateFactory.CreateAuthenticationProtocolRun(inputs)
}
func (factory *delayedForwardFactory) releaseCreation() {
	factory.once.Do(func() { close(factory.release) })
}

func installTestForwarder(sup *Supervisor, id ID, actor *session.AuthenticationSession) *managedSession {
	managed := &managedSession{actor: actor, intent: session.MaintainAuthentication, state: stateActive, stateChange: make(chan struct{}), stopForwarding: make(chan struct{}), forwardDone: make(chan struct{})}
	sup.mu.Lock()
	sup.sessions[id] = managed
	sup.mu.Unlock()
	sup.revisionForwarders.Add(1)
	go func() { defer close(managed.forwardDone); sup.forwardRevisions(id, managed) }()
	return managed
}

func TestSupervisorDelayedForwardUsesPublishedSnapshot(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer closeTestSupervisor(sup)
	events := subscribeForTest(t, sup)
	factory := &delayedForwardFactory{stateFactory: newStateFactory(), entered: make(chan struct{}), release: make(chan struct{})}
	defer factory.releaseCreation()
	definition := testRuntimeDefinition()
	definition.Configuration.AuthenticationSessionID = "paired"
	definition.AuthenticationProtocolFactory = factory
	actor, err := session.NewAuthenticationSession(definition, session.MaintainAuthentication, sup.sessionDependencies)
	if err != nil {
		t.Fatal(err)
	}
	actor.Start()
	defer func() {
		factory.releaseCreation()
		if err := actor.Shutdown(context.Background()); err != nil && !errors.Is(err, session.ErrAuthenticationSessionClosed) {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	initial, err := actor.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	apply := make(chan error, 1)
	go func() {
		_, err := actor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1))
		apply <- err
	}()
	waitForSignal(t, factory.entered, "Factory entered")
	installTestForwarder(sup, "paired", actor)
	// The binding publication is consumed while the actor remains in Factory.
	// Its latest-Snapshot query can reply only after the later Run reset commits.
	for len(actor.RevisionEvents()) != 0 {
		select {
		case <-ctx.Done():
			t.Fatal("forwarder did not consume binding publication")
		default:
			runtime.Gosched()
		}
	}
	first := nextForTest(t, ctx, events)
	factory.releaseCreation()
	select {
	case err := <-apply:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if first.Revision != initial.Revision+1 || first.Snapshot.State != session.WaitingForNetwork || first.Snapshot.ProtocolSocket.RunGeneration != 0 || first.Snapshot.SelectedNetworkBinding == nil {
		t.Fatalf("delayed binding event acquired later Run contents: %#v", first)
	}
	if first.Revision != first.Snapshot.Revision || first.SessionID != first.Snapshot.AuthenticationSessionID {
		t.Fatal("delayed event revision/content mismatch")
	}
	next := nextForTest(t, ctx, events)
	if next.Revision != first.Revision+1 || next.Revision != next.Snapshot.Revision || next.SessionID != next.Snapshot.AuthenticationSessionID || next.Snapshot.State != session.Authenticating || next.Snapshot.ProtocolSocket.RunGeneration != 1 {
		t.Fatalf("new Run paired event = %#v", next)
	}
}

func TestSupervisorForwardsPublishedEventAfterActorQueryCloses(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer closeTestSupervisor(sup)
	events := subscribeForTest(t, sup)
	definition := testRuntimeDefinition()
	definition.Configuration.AuthenticationSessionID = "closed-paired"
	actor, err := session.NewAuthenticationSession(definition, session.SuspendAuthentication, sup.sessionDependencies)
	if err != nil {
		t.Fatal(err)
	}
	actor.Start()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	published, err := actor.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := actor.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	managed := installTestForwarder(sup, "closed-paired", actor)
	event := nextForTest(t, ctx, events)
	if event.Revision != published.Revision || event.SessionID != published.AuthenticationSessionID || !reflect.DeepEqual(event.Snapshot, published) {
		t.Fatalf("closed actor publication = %#v", event)
	}
	select {
	case <-managed.forwardDone:
	case <-ctx.Done():
		t.Fatal("forwarder did not exit closed producer channel")
	}
}

func TestSupervisorSubscriberClonesOwnEveryNestedSnapshotField(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer closeTestSupervisor(sup)
	left, right := subscribeForTest(t, sup), subscribeForTest(t, sup)
	now := time.Unix(100, 0)
	snapshot := Snapshot{AuthenticationSessionID: "clone", Revision: 9, StateReason: &session.StateReason{Code: "test", Description: "original"}, SelectedNetworkBinding: &session.NetworkBindingSummary{DisplayName: "original"}, AuthenticationEstablishedAt: &now, NextRetryAt: &now, LastAuthenticationFailure: &session.AuthenticationFailure{Code: "original"}, ProtocolSocket: session.ProtocolSocketObservation{RunGeneration: 1, State: session.ProtocolSocketClosed, LocalEndpoint: netip.MustParseAddrPort("127.0.0.1:40001"), RemoteEndpoint: netip.MustParseAddrPort("127.0.0.1:61440")}}
	original := snapshot.Clone()
	sup.publishStateEvent(StateEvent{Kind: StateSessionChanged, SessionID: snapshot.AuthenticationSessionID, Revision: snapshot.Revision, Snapshot: snapshot})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, second := nextForTest(t, ctx, left), nextForTest(t, ctx, right)
	first.Snapshot.StateReason.Description = "changed"
	first.Snapshot.SelectedNetworkBinding.DisplayName = "changed"
	*first.Snapshot.AuthenticationEstablishedAt = time.Time{}
	*first.Snapshot.NextRetryAt = time.Time{}
	first.Snapshot.LastAuthenticationFailure.Code = "changed"
	first.Snapshot.ProtocolSocket.State = session.ProtocolSocketOpen
	if !reflect.DeepEqual(second.Snapshot, original) || !reflect.DeepEqual(snapshot, original) {
		t.Fatal("receiver mutated another subscriber or source Snapshot")
	}
	snapshot.StateReason.Description = "source changed"
	if !reflect.DeepEqual(second.Snapshot, original) {
		t.Fatal("source mutated published clone")
	}
}

type shutdownSignalDiagnostics struct {
	session.NoopDiagnostics
	shutdown chan struct{}
	once     sync.Once
}

func (diagnostics *shutdownSignalDiagnostics) SessionCommand(command string) {
	if command == "shutdown" {
		diagnostics.once.Do(func() { close(diagnostics.shutdown) })
	}
}

func TestSupervisorRemovalDrainsQueuedFinalRevisionBeforeTerminal(t *testing.T) {
	for _, operation := range []string{"remove", "forget", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			sup := New(testSupervisorDeps())
			stream := subscribeForTest(t, sup)
			diagnostics := &shutdownSignalDiagnostics{shutdown: make(chan struct{})}
			deps := sup.sessionDependencies
			deps.Diagnostics = diagnostics
			definition := testRuntimeDefinition()
			definition.Configuration.AuthenticationSessionID = "drain"
			actor, err := session.NewAuthenticationSession(definition, session.SuspendAuthentication, deps)
			if err != nil {
				t.Fatal(err)
			}
			actor.Start()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			initial, err := actor.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			latest, err := actor.ApplySystemNetworkSnapshot(ctx, testNetworkSnapshot(1))
			if err != nil {
				t.Fatal(err)
			}
			if latest.Revision <= initial.Revision {
				t.Fatal("test requires a newer queued actor revision")
			}
			managed := &managedSession{actor: actor, intent: session.SuspendAuthentication, state: stateStopped, stateChange: make(chan struct{}), stopForwarding: make(chan struct{}), forwardDone: make(chan struct{})}
			sup.mu.Lock()
			sup.sessions["drain"] = managed
			sup.mu.Unlock()
			sup.revisionForwarders.Add(1)
			var once sync.Once
			startForwarder := func() {
				once.Do(func() { go func() { defer close(managed.forwardDone); sup.forwardRevisions("drain", managed) }() })
			}
			defer func() { startForwarder(); sup.Close(); sup.Wait() }()
			cause := errors.New("original initial snapshot failure")
			result := make(chan error, 1)
			go func() {
				switch operation {
				case "remove":
					result <- sup.Remove(ctx, "drain")
				case "forget":
					result <- sup.ForgetStopped("drain")
				case "rollback":
					result <- sup.rollbackNewSession("drain", managed, cause)
				}
			}()
			waitForSignal(t, diagnostics.shutdown, "actor shutdown begins with newer revision queued")
			// The map and forwarder stay owned until the producer queue is drained.
			sup.mu.Lock()
			current := sup.sessions["drain"]
			sup.mu.Unlock()
			if current != managed {
				t.Fatal("deleted before forwarder drain")
			}
			if err := sup.deliverNetworkSnapshot(ctx, testNetworkSnapshot(2), []networkSnapshotTarget{{id: "drain", actor: actor}}); err != nil {
				t.Fatalf("network delivery interrupted by retiring actor: %v", err)
			}
			select {
			case <-managed.stopForwarding:
				t.Fatal("normal removal stopped forwarder before drain")
			default:
			}
			startForwarder()
			select {
			case err := <-result:
				if operation == "rollback" {
					if !errors.Is(err, cause) {
						t.Fatal("start failure classification lost", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("removal did not finish drain")
			}
			event := nextForTest(t, ctx, stream)
			if event.Kind != StateSessionRemoved || event.SessionID != "drain" || event.Revision != latest.Revision || event.Snapshot != (Snapshot{}) {
				t.Fatalf("final terminal regressed or fabricated: %#v latest=%#v", event, latest)
			}
			select {
			case <-managed.forwardDone:
			default:
				t.Fatal("terminal preceded forwarder exit")
			}
			if _, err := sup.Get(ctx, "drain"); !errors.Is(err, ErrSessionNotFound) {
				t.Fatal("successful removal retained actor", err)
			}
			quiet, quietCancel := context.WithTimeout(ctx, 20*time.Millisecond)
			defer quietCancel()
			if event, err := stream.Next(quiet); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("publication followed terminal", event, err)
			}
		})
	}
}
