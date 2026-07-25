package supervisor

import (
	"context"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	profile "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	environment "sidravia/internal/daemon/environment"
)

type stubFactory struct{}

func (stubFactory) ProtocolID() protocol.AuthenticationProtocolID { return "test-protocol" }
func (stubFactory) ValidateInstitutionProtocolConfiguration(protocol.InstitutionProtocolConfiguration) error {
	return nil
}
func (stubFactory) ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride) error {
	return nil
}
func (stubFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return blockingRun{}, nil
}

type blockingRun struct{}

func (blockingRun) Execute(ctx context.Context, _ protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	<-ctx.Done()
	return nil
}

type unavailableRetryPolicy struct{}

func (unavailableRetryPolicy) Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool) {
	return 0, false
}

type noOpRetryScheduler struct{}

func (noOpRetryScheduler) Schedule(time.Duration, func()) session.RetryCancellation {
	return noOpRetryCancellation{}
}

type noOpRetryCancellation struct{}

func (noOpRetryCancellation) Cancel() {}

func testSupervisorDeps() Dependencies {
	return Dependencies{
		Now:            func() time.Time { return time.Unix(100, 0) },
		RetryPolicy:    unavailableRetryPolicy{},
		RetryScheduler: noOpRetryScheduler{},
	}
}

func testRuntimeDefinition() session.RuntimeDefinition {
	return session.RuntimeDefinition{
		Configuration: session.Configuration{
			DisplayName:          "Campus network",
			InstitutionProfileID: "profile-1",
			CredentialID:         "credential-1",
			NetworkBindingPolicy: session.NetworkBindingPolicy{
				Mode: session.AutomaticallySelectLatestAvailable,
			},
			ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride([]byte(`{"network":"campus"}`)),
		},
		AuthenticationCredential: credential.AuthenticationCredential{
			Username: "test-account",
			Password: "secret-password",
		},
		InstitutionProfile: profile.InstitutionProfile{
			InstitutionProfileID:             "profile-1",
			DisplayName:                      "Campus",
			AuthenticationProtocolID:         "test-protocol",
			InstitutionProtocolConfiguration: protocol.InstitutionProtocolConfiguration([]byte(`{"realm":"campus"}`)),
		},
		AuthenticationProtocolFactory: stubFactory{},
		SystemHostInformation: environment.SystemHostInformation{
			HostName:              "test-host",
			OperatingSystemFamily: "test-os",
			MachineArchitecture:   "test-architecture",
		},
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

func TestSupervisorRejectsSecondActiveMaintainAuthentication(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

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
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	ctx := context.Background()
	id, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err != nil {
		t.Fatalf("StartResolved (1) error: %v", err)
	}

	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved (2) after stop error: %v", err)
	}
}

func TestSupervisorRestartAdmitsStoppedSession(t *testing.T) {
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

	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	snapshot, err := supervisor.Restart(ctx, id)
	if err != nil {
		t.Fatalf("Restart error: %v", err)
	}
	if snapshot.State == session.Suspended {
		t.Errorf("state after Restart = %q, want not Suspended", snapshot.State)
	}
}

func TestSupervisorRestartRejectsActiveSession(t *testing.T) {
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

	if _, err := supervisor.Restart(ctx, id); err == nil {
		t.Fatal("expected error for Restart on active session, got nil")
	}
}

func TestSupervisorForgetStoppedRemovesSession(t *testing.T) {
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

	if _, err := supervisor.Stop(ctx, id); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	if err := supervisor.ForgetStopped(id); err != nil {
		t.Fatalf("ForgetStopped error: %v", err)
	}

	if _, err := supervisor.Get(ctx, id); err == nil {
		t.Fatal("expected error for Get after ForgetStopped, got nil")
	}
}

func TestSupervisorForgetStoppedRejectsActiveSession(t *testing.T) {
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

	if err := supervisor.ForgetStopped(id); err == nil {
		t.Fatal("expected error for ForgetStopped on active session, got nil")
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

	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved (2) error: %v", err)
	}

	snapshots, err := supervisor.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Errorf("len(snapshots) = %d, want 2", len(snapshots))
	}
}

func TestSupervisorRevisionEventsReceivesUpdates(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	events := supervisor.RevisionEvents()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved error: %v", err)
	}

	select {
	case event := <-events:
		if event.SessionID != "session-1" {
			t.Errorf("event SessionID = %q, want %q", event.SessionID, "session-1")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for revision event")
	}
}

func TestSupervisorSlowSubscriberDoesNotBlock(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

	_ = supervisor.RevisionEvents()

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

		restartCtx, restartCancel := context.WithTimeout(ctx, time.Second)
		if _, err := supervisor.Restart(restartCtx, id); err != nil {
			restartCancel()
			t.Fatalf("Restart iteration %d error: %v", i, err)
		}
		restartCancel()
	}
}

func TestSupervisorCloseShutsDownAllSessions(t *testing.T) {
	supervisor := New(testSupervisorDeps())

	ctx := context.Background()
	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.SuspendAuthentication); err != nil {
		t.Fatalf("StartResolved (1) error: %v", err)
	}
	if _, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); err != nil {
		t.Fatalf("StartResolved (2) error: %v", err)
	}

	if err := supervisor.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	supervisor.Wait()

	getCtx, getCancel := context.WithTimeout(context.Background(), time.Second)
	defer getCancel()
	if _, err := supervisor.Get(getCtx, "session-1"); err == nil {
		t.Fatal("expected error for Get after Close, got nil")
	}
}

func TestSupervisorRejectsStartAfterClose(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	_ = supervisor.Close()
	supervisor.Wait()

	ctx := context.Background()
	_, _, err := supervisor.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication)
	if err == nil {
		t.Fatal("expected error for StartResolved after Close, got nil")
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

// TestSupervisorCloseRevisionChannelRace verifies that Close does not panic
// by sending on a closed subscriber channel. The fix ensures Close waits for
// all forward goroutines before closing subscriber channels.
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

		events := supervisor.RevisionEvents()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range events {
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

	// ForgetStopped shuts down the session and closes stopFwd, which should
	// cause the forward goroutine to exit.
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
// StartResolved with a pre-cancelled context returns an error, leaves no
// residual sessions, and allows a subsequent MaintainAuthentication start.
func TestSupervisorPreCancelledContextReturnsErrorAndCleansUp(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer func() {
		_ = supervisor.Close()
		supervisor.Wait()
	}()

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
