package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
)

func TestSupervisorRejectsSecondActiveMaintainAuthentication(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx := context.Background()
	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved (1) error: %v", err)
	}

	_, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err == nil {
		t.Fatal("expected admission error for second MaintainAuthentication, got nil")
	}
}

func TestSupervisorAllowsNewSessionAfterStop(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved (1) error: %v", err)
	}

	snapshot, err := supervisor.Stop(ctx, id)
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if snapshot.State != session.Stopping {
		t.Fatalf("Stop state = %q, want %q", snapshot.State, session.Stopping)
	}
	suspended := waitForSupervisorState(t, supervisor, id, session.Suspended)
	repeated, err := supervisor.Stop(ctx, id)
	if err != nil {
		t.Fatalf("repeated Stop after suspended error: %v", err)
	}
	if repeated.State != session.Suspended || repeated.Revision != suspended.Revision {
		t.Fatalf("repeated Stop after suspended = %#v, want unchanged revision %d", repeated, suspended.Revision)
	}

	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved (2) after stop error: %v", err)
	}
}

func TestSupervisorPreCanceledStopRollsAdmissionBackToActive(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	id, _, err := supervisor.StartResolved(context.Background(), testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := supervisor.Stop(canceled, id); err == nil {
		t.Fatal("pre-canceled Stop error = nil, want error")
	}

	supervisor.mu.Lock()
	state := supervisor.sessions[id].state
	supervisor.mu.Unlock()
	if state != stateActive {
		t.Fatalf("state after rejected Stop = %v, want stateActive", state)
	}

	stopping, err := supervisor.Stop(context.Background(), id)
	if err != nil {
		t.Fatalf("subsequent Stop error: %v", err)
	}
	if stopping.State != session.Stopping {
		t.Fatalf("subsequent Stop state = %q, want %q", stopping.State, session.Stopping)
	}
	waitForSupervisorState(t, supervisor, id, session.Suspended)
}

func TestSupervisorRestartAdmitsStoppedSession(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	waitForSupervisorState(t, supervisor, id, session.Suspended)

	snapshot, err := supervisor.Restart(ctx, id)
	if err != nil {
		t.Fatalf("Restart error: %v", err)
	}
	if snapshot.State == session.Suspended {
		t.Errorf("state after Restart = %q, want not Suspended", snapshot.State)
	}
}

func TestSupervisorRestartAcceptsActiveSession(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	if _, err := supervisor.Restart(ctx, id); err != nil {
		t.Fatalf("Restart active session error: %v", err)
	}
}

func TestSupervisorEnsureRunningSuspendedAndRemove(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer supervisor.Close()
	id, _, err := supervisor.StartResolved(context.Background(), testRuntimeDefinition(), session.SuspendAuthentication)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.EnsureRunning(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Stop(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	waitForSupervisorState(t, supervisor, id, session.Suspended)
	if err := supervisor.Remove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Get(context.Background(), id); err == nil {
		t.Fatal("removed session remained observable")
	}
}

func TestSupervisorRemoveStoppedWhileAnotherSessionActive(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)
	ctx := context.Background()

	sessionA, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("start Session A: %v", err)
	}
	if _, err := supervisor.Stop(ctx, sessionA); err != nil {
		t.Fatalf("stop Session A: %v", err)
	}
	waitForSupervisorState(t, supervisor, sessionA, session.Suspended)

	sessionB, before, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("start Session B: %v", err)
	}
	if err := supervisor.Remove(ctx, sessionA); err != nil {
		t.Fatalf("remove stopped Session A while Session B is active: %v", err)
	}
	if _, err := supervisor.Get(ctx, sessionA); err == nil {
		t.Fatal("Get(Session A) succeeded after removal")
	}
	listed, err := supervisor.List(ctx)
	if err != nil {
		t.Fatalf("List after removal: %v", err)
	}
	if len(listed) != 1 || listed[0].AuthenticationSessionID != sessionB {
		t.Fatalf("List after removal = %#v, want only Session B", listed)
	}
	after, err := supervisor.Get(ctx, sessionB)
	if err != nil {
		t.Fatalf("Get(Session B): %v", err)
	}
	if after.Revision != before.Revision || after.State != before.State {
		t.Fatalf("Session B changed during removal: before=%#v after=%#v", before, after)
	}
}

func TestSupervisorStoppingEnsureWaitsAndHoldsAdmission(t *testing.T) {
	factory := &sequentialHeldFactory{created: make(chan *heldCancellationRun, 8)}
	supervisor := New(testSupervisorDeps())
	defer func() {
		factory.releaseAll()
		_ = supervisor.Close()
		supervisor.Wait()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	definition := testRuntimeDefinition()
	definition.AuthenticationProtocolFactory = factory
	id, _, err := supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		t.Fatal(err)
	}
	oldRun := factory.waitForRun(t)
	<-oldRun.started
	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	<-oldRun.canceled
	result := make(chan error, 1)
	go func() { _, err := supervisor.EnsureRunning(ctx, id); result <- err }()
	waitForSupervisorReservation(t, supervisor, id, true)
	select {
	case err := <-result:
		t.Fatalf("ensure returned before cleanup release: %v", err)
	default:
	}
	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err == nil {
		t.Fatal("competing start stole admission during stopping ensure")
	}
	if factory.run(1) != nil {
		t.Fatal("ensure started replacement before old cleanup exited")
	}
	releaseHeldCancellationRun(oldRun)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	replacement := factory.waitForRun(t)
	waitForSignal(t, replacement.started, "replacement Run start")
	if factory.run(2) != nil {
		t.Fatal("ensure created more than one replacement Run")
	}
	releaseHeldCancellationRun(replacement)
}

func TestSupervisorStoppingRestartWaitsAndDoesNotOverlap(t *testing.T) {
	factory := &sequentialHeldFactory{created: make(chan *heldCancellationRun, 8)}
	supervisor := New(testSupervisorDeps())
	defer func() {
		factory.releaseAll()
		_ = supervisor.Close()
		supervisor.Wait()
	}()
	ctx := context.Background()
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	definition := testRuntimeDefinition()
	definition.AuthenticationProtocolFactory = factory
	id, _, err := supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		t.Fatal(err)
	}
	oldRun := factory.waitForRun(t)
	waitForSignal(t, oldRun.started, "old Run start")
	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, oldRun.canceled, "old Run cancellation")
	result := make(chan error, 1)
	go func() { _, err := supervisor.Restart(ctx, id); result <- err }()
	waitForSupervisorReservation(t, supervisor, id, true)
	if factory.run(1) != nil {
		t.Fatal("restart overlapped replacement with old cleanup")
	}
	select {
	case err := <-result:
		t.Fatalf("restart returned before old cleanup: %v", err)
	default:
	}
	releaseHeldCancellationRun(oldRun)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	replacement := factory.waitForRun(t)
	waitForSignal(t, replacement.started, "replacement Run start")
	if factory.run(2) != nil {
		t.Fatal("restart created more than one replacement")
	}
	releaseHeldCancellationRun(replacement)
}

func TestSupervisorRemoveOrderingAndReadmission(t *testing.T) {
	for _, test := range []struct {
		name             string
		stopFirst        bool
		suspendInitially bool
	}{
		{name: "online"},
		{name: "already-stopping", stopFirst: true},
		{name: "suspended", suspendInitially: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			factory := &sequentialHeldFactory{created: make(chan *heldCancellationRun, 8)}
			supervisor := New(testSupervisorDeps())
			defer func() {
				factory.releaseAll()
				_ = supervisor.Close()
				supervisor.Wait()
			}()
			ctx := context.Background()
			definition := testRuntimeDefinition()
			definition.AuthenticationProtocolFactory = factory
			intent := session.MaintainAuthentication
			if test.suspendInitially {
				intent = session.SuspendAuthentication
			} else if err := supervisor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1)); err != nil {
				t.Fatal(err)
			}
			id, _, err := supervisor.StartResolved(ctx, definition, intent)
			if err != nil {
				t.Fatal(err)
			}
			if test.suspendInitially {
				if factory.run(0) != nil {
					t.Fatal("suspended remove created a protocol Run")
				}
				if err := supervisor.Remove(ctx, id); err != nil {
					t.Fatal(err)
				}
			} else {
				run := factory.waitForRun(t)
				waitForSignal(t, run.started, "Run start")
				if test.stopFirst {
					if _, err := supervisor.Stop(ctx, id); err != nil {
						t.Fatal(err)
					}
					waitForSignal(t, run.canceled, "accepted Stop cancellation")
				}
				done := make(chan error, 1)
				go func() { done <- supervisor.Remove(ctx, id) }()
				waitForSupervisorReservation(t, supervisor, id, true)
				select {
				case err := <-done:
					t.Fatalf("remove completed before cleanup release: %v", err)
				default:
				}
				if factory.run(1) != nil {
					t.Fatal("remove created a second Run")
				}
				if !test.stopFirst {
					waitForSignal(t, run.canceled, "remove cancellation")
				}
				releaseHeldCancellationRun(run)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := supervisor.Get(ctx, id); err == nil {
				t.Fatal("removed Session remained in Get")
			}
			listed, err := supervisor.List(ctx)
			if err != nil || len(listed) != 0 {
				t.Fatalf("List after remove=%#v err=%v", listed, err)
			}
			nextID, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
			if err != nil || nextID == "" {
				t.Fatalf("later admission failed: id=%q err=%v", nextID, err)
			}
		})
	}
}

func TestSupervisorEnsureRunningPreservesEveryActivePublicState(t *testing.T) {
	tests := []struct {
		name  string
		state session.State
		setup func(*testing.T, *Supervisor, *stateFactory, *supervisorRetryScheduler, ID)
	}{
		{"waiting_for_network", session.WaitingForNetwork, func(*testing.T, *Supervisor, *stateFactory, *supervisorRetryScheduler, ID) {}},
		{"authenticating", session.Authenticating, func(t *testing.T, _ *Supervisor, factory *stateFactory, _ *supervisorRetryScheduler, _ ID) {
			_ = factory.wait(t)
		}},
		{"authenticated", session.Authenticated, func(t *testing.T, _ *Supervisor, factory *stateFactory, _ *supervisorRetryScheduler, _ ID) {
			run := factory.wait(t)
			select {
			case observer := <-run.observer:
				observer.AuthenticationEstablished()
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for Run observer")
			}
		}},
		{"waiting_before_retry", session.WaitingBeforeRetry, func(t *testing.T, _ *Supervisor, factory *stateFactory, _ *supervisorRetryScheduler, _ ID) {
			run := factory.wait(t)
			run.finish <- &protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay}
		}},
		{"blocked_by_error", session.BlockedByError, func(t *testing.T, _ *Supervisor, factory *stateFactory, _ *supervisorRetryScheduler, _ ID) {
			run := factory.wait(t)
			run.finish <- &protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler := &supervisorRetryScheduler{}
			deps := testSupervisorDeps()
			deps.RetryScheduler = scheduler
			if test.state == session.WaitingBeforeRetry {
				deps.RetryPolicy = supervisorRetryPolicy{delay: time.Minute, ok: true}
			}
			supervisor := New(deps)
			defer closeTestSupervisor(supervisor)
			factory := newStateFactory()
			definition := testRuntimeDefinition()
			definition.AuthenticationProtocolFactory = factory
			if test.state != session.WaitingForNetwork {
				if err := supervisor.ApplySystemNetworkSnapshot(context.Background(), supervisorUsableNetworkSnapshot(t, 1)); err != nil {
					t.Fatal(err)
				}
			}
			id, _, err := supervisor.StartResolved(context.Background(), definition, session.MaintainAuthentication)
			if err != nil {
				t.Fatal(err)
			}
			test.setup(t, supervisor, factory, scheduler, id)
			before := waitForSupervisorState(t, supervisor, id, test.state)
			runCount := factory.count()
			retryCount := scheduler.count()
			got, err := supervisor.EnsureRunning(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != before.State || got.Revision != before.Revision {
				t.Fatalf("EnsureRunning changed active snapshot: before=%#v after=%#v", before, got)
			}
			if factory.count() != runCount {
				t.Fatalf("EnsureRunning created another Run: before=%d after=%d", runCount, factory.count())
			}
			if scheduler.count() != retryCount {
				t.Fatalf("EnsureRunning consumed retry callback: before=%d after=%d", retryCount, scheduler.count())
			}
			if runCount > 0 {
				factory.mu.Lock()
				current := factory.runs[runCount-1]
				factory.mu.Unlock()
				select {
				case <-current.canceled:
					t.Fatal("EnsureRunning canceled current Run")
				default:
				}
			}
		})
	}
}

func TestSupervisorSerializesStopQueuedRestartAndRemoveForSameID(t *testing.T) {
	factory := &sequentialHeldFactory{created: make(chan *heldCancellationRun, 8)}
	supervisor := New(testSupervisorDeps())
	defer func() {
		factory.releaseAll()
		_ = supervisor.Close()
		supervisor.Wait()
	}()
	ctx := context.Background()
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	definition := testRuntimeDefinition()
	definition.AuthenticationProtocolFactory = factory
	id, _, err := supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		t.Fatal(err)
	}
	oldRun := factory.waitForRun(t)
	waitForSignal(t, oldRun.started, "old Run start")
	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, oldRun.canceled, "old Run cancellation")

	restartDone := make(chan error, 1)
	go func() { _, err := supervisor.Restart(ctx, id); restartDone <- err }()
	waitForSupervisorReservation(t, supervisor, id, true)
	removeDone := make(chan error, 1)
	go func() { removeDone <- supervisor.Remove(ctx, id) }()
	releaseHeldCancellationRun(oldRun)
	if err := <-restartDone; err != nil {
		t.Fatalf("queued restart: %v", err)
	}
	replacement := factory.waitForRun(t)
	waitForSignal(t, replacement.started, "replacement Run start")
	waitForSignal(t, replacement.canceled, "replacement cancellation by queued remove")
	releaseHeldCancellationRun(replacement)
	if err := <-removeDone; err != nil {
		t.Fatalf("queued remove: %v", err)
	}
	if factory.run(2) != nil {
		t.Fatal("serialized commands created more than one replacement Run")
	}
	if _, err := supervisor.Get(ctx, id); err == nil {
		t.Fatal("successful remove left Session or forwarder observable")
	}
}

func TestSupervisorCanceledWaitingOperationsDoNotRunLater(t *testing.T) {
	for _, operation := range []struct {
		name string
		call func(context.Context, *Supervisor, ID) error
	}{
		{"ensure", func(ctx context.Context, supervisor *Supervisor, id ID) error {
			_, err := supervisor.EnsureRunning(ctx, id)
			return err
		}},
		{"restart", func(ctx context.Context, supervisor *Supervisor, id ID) error {
			_, err := supervisor.Restart(ctx, id)
			return err
		}},
		{"remove", func(ctx context.Context, supervisor *Supervisor, id ID) error { return supervisor.Remove(ctx, id) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			run := newHeldCancellationRun()
			supervisor := New(testSupervisorDeps())
			defer func() {
				releaseHeldCancellationRun(run)
				_ = supervisor.Close()
				supervisor.Wait()
			}()
			ctx := context.Background()
			if err := supervisor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1)); err != nil {
				t.Fatal(err)
			}
			definition := testRuntimeDefinition()
			definition.AuthenticationProtocolFactory = heldCancellationFactory{run: run}
			id, _, err := supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
			if err != nil {
				t.Fatal(err)
			}
			<-run.started
			if _, err := supervisor.Stop(ctx, id); err != nil {
				t.Fatal(err)
			}
			<-run.canceled
			waitCtx, cancel := context.WithCancel(ctx)
			result := make(chan error, 1)
			go func() { result <- operation.call(waitCtx, supervisor, id) }()
			waitForSupervisorReservation(t, supervisor, id, true)
			cancel()
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("waiting operation error=%v", err)
			}
			waitForSupervisorReservation(t, supervisor, id, false)
			if factory, ok := definition.AuthenticationProtocolFactory.(heldCancellationFactory); !ok || factory.run != run {
				t.Fatal("test factory changed unexpectedly")
			}
			releaseHeldCancellationRun(run)
			suspended := waitForSupervisorState(t, supervisor, id, session.Suspended)
			if suspended.State != session.Suspended {
				t.Fatal("Session did not remain suspended")
			}
			if _, err := supervisor.Get(ctx, id); err != nil {
				t.Fatalf("canceled operation deleted Session later: %v", err)
			}
		})
	}
}

func waitForSupervisorReservation(t *testing.T, supervisor *Supervisor, id ID, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		supervisor.mu.Lock()
		managed := supervisor.sessions[id]
		got := managed != nil && managed.reserved
		supervisor.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for reserved=%v", want)
}

func TestSupervisorForgetStoppedRemovesSession(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	waitForSupervisorState(t, supervisor, id, session.Suspended)

	if err := supervisor.ForgetStopped(id); err != nil {
		t.Fatalf("ForgetStopped error: %v", err)
	}

	if _, err := supervisor.Get(ctx, id); err == nil {
		t.Fatal("expected error for Get after ForgetStopped, got nil")
	}
}

func TestSupervisorForgetStoppedRejectsActiveSession(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	if err := supervisor.ForgetStopped(id); err == nil {
		t.Fatal("expected error for ForgetStopped on active session, got nil")
	}
}

// TestSupervisorStopStartRace verifies that concurrent Stop and StartResolved
// cannot create two active MaintainAuthentication sessions. The stateStopping
// marker, set under lock before releasing for Suspend, prevents the race window
// where a second StartResolved could see a stale stopped state.
func TestSupervisorStopStartRace(t *testing.T) {
	for round := 0; round < 100; round++ {
		supervisor := New(testSupervisorDeps())

		ctx := context.Background()
		id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
		if err != nil {
			supervisor.Close()
			supervisor.Wait()
			t.Fatalf("round %d: StartResolved error: %v", round, err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			supervisor.Stop(ctx, id)
		}()

		go func() {
			defer wg.Done()
			supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
		}()

		wg.Wait()

		// Verify at most one non-suspended session exists.
		snapshots, err := supervisor.List(ctx)
		if err != nil {
			supervisor.Close()
			supervisor.Wait()
			t.Fatalf("round %d: List error: %v", round, err)
		}
		activeCount := 0
		for _, snap := range snapshots {
			if snap.State != session.Suspended {
				activeCount++
			}
		}
		if activeCount > 1 {
			t.Errorf("round %d: %d non-suspended sessions, want <= 1", round, activeCount)
		}

		supervisor.Close()
		supervisor.Wait()
	}
}

// StartResolved with a pre-cancelled context returns an error, leaves no
// residual sessions, and allows a subsequent MaintainAuthentication start.
func TestSupervisorPreCancelledContextReturnsErrorAndCleansUp(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err == nil {
		t.Fatal("expected error from pre-cancelled context, got nil")
	}

	snapshots, err := supervisor.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("expected empty list after rollback, got %d entries", len(snapshots))
	}

	ctx2 := context.Background()
	id, _, err := supervisor.StartResolved(ctx2, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("subsequent StartResolved error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session id")
	}
}

func TestSupervisorKeepsAdmissionClosedUntilProtocolRunExits(t *testing.T) {
	run := newHeldCancellationRun()
	supervisor := New(testSupervisorDeps())
	defer func() {
		releaseHeldCancellationRun(run)
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	if err := supervisor.ApplySystemNetworkSnapshot(ctx, supervisorUsableNetworkSnapshot(t, 1)); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot error: %v", err)
	}

	definition := testRuntimeDefinition()
	definition.AuthenticationProtocolFactory = heldCancellationFactory{run: run}
	id, _, err := supervisor.StartResolved(ctx, definition, session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}
	waitForSignal(t, run.started, "protocol run start")

	first, err := supervisor.Stop(ctx, id)
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if first.State != session.Stopping {
		t.Fatalf("first Stop state = %q, want %q", first.State, session.Stopping)
	}
	waitForSignal(t, run.canceled, "protocol run cancellation")

	supervisor.mu.Lock()
	managed := supervisor.sessions[id]
	supervisor.mu.Unlock()
	supervisor.reconcileStopError(id, managed)
	supervisor.mu.Lock()
	state := managed.state
	supervisor.mu.Unlock()
	if state != stateStopping {
		t.Fatalf("state after reconciling accepted Stop = %v, want stateStopping", state)
	}

	repeated, err := supervisor.Stop(ctx, id)
	if err != nil {
		t.Fatalf("repeated Stop error: %v", err)
	}
	if repeated.Revision != first.Revision || repeated.State != session.Stopping {
		t.Fatalf("repeated Stop = revision %d state %q, want revision %d state %q", repeated.Revision, repeated.State, first.Revision, session.Stopping)
	}
	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err == nil {
		t.Fatal("StartResolved during stopping succeeded, want admission error")
	}

	releaseHeldCancellationRun(run)
	waitForSupervisorState(t, supervisor, id, session.Suspended)

	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved after suspended error: %v", err)
	}
}
