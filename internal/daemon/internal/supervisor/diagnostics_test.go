package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/internal/session"
)

// supervisorCaptureDiagnostics records whether the Session diagnostics sink is
// invoked. It implements session.Diagnostics.
type supervisorCaptureDiagnostics struct {
	mu        sync.Mutex
	snapshots int
}

func (d *supervisorCaptureDiagnostics) SessionSnapshot(session.Snapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshots++
}

func (d *supervisorCaptureDiagnostics) SessionCommand(string)        {}
func (d *supervisorCaptureDiagnostics) ProtocolRunGeneration(uint64) {}
func (d *supervisorCaptureDiagnostics) RetryScheduled(time.Time)     {}

// TestSupervisorPassesDiagnosticsToSession proves the Supervisor wires its
// Diagnostics dependency into each created Session so the sink observes the
// initial committed revision.
func TestSupervisorPassesDiagnosticsToSession(t *testing.T) {
	capture := &supervisorCaptureDiagnostics{}
	deps := testSupervisorDeps()
	deps.Diagnostics = capture
	supervisor := New(deps)
	defer closeTestSupervisor(supervisor)

	if _, _, err := supervisor.StartResolved(context.Background(), testRuntimeDefinition(), session.SuspendAuthentication); err != nil {
		t.Fatalf("StartResolved: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		capture.mu.Lock()
		n := capture.snapshots
		capture.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("diagnostics sink was not invoked for session creation")
}

func TestSupervisorDiagnosticSnapshotMissingRetainedAndReadOnly(t *testing.T) {
	supervisor := New(testSupervisorDeps())
	defer closeTestSupervisor(supervisor)
	if _, err := supervisor.GetDiagnosticSnapshot(context.Background(), "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing = %v", err)
	}
	factory := newStateFactory()
	definition := testRuntimeDefinition()
	definition.AuthenticationProtocolFactory = factory
	id, before, err := supervisor.StartResolved(context.Background(), definition, session.SuspendAuthentication)
	if err != nil {
		t.Fatal(err)
	}
	got, err := supervisor.GetDiagnosticSnapshot(context.Background(), id)
	if err != nil || got.Snapshot.Revision != before.Revision || got.Snapshot.State != session.Suspended || got.ProtocolSocket.State != session.ProtocolSocketNotObserved || got.ProtocolSocket.RunGeneration != 0 || factory.count() != 0 {
		t.Fatalf("retained query = %#v, %v; runs=%d", got, err, factory.count())
	}
	cause := errors.New("diagnostic query canceled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	if _, err := supervisor.GetDiagnosticSnapshot(ctx, id); !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
		t.Fatalf("actor cause = %v", err)
	}
	if _, err := supervisor.GetDiagnosticSnapshot(nil, id); err == nil {
		t.Fatal("nil actor context accepted")
	}
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.GetDiagnosticSnapshot(context.Background(), id); !errors.Is(err, session.ErrAuthenticationSessionClosed) {
		t.Fatalf("closed actor = %v", err)
	}
	if factory.count() != 0 {
		t.Fatal("diagnostics started Run")
	}
}
