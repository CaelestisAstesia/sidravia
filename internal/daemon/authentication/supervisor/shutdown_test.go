package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/session"
)

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

func TestSupervisorFailedStartPublishesTerminalAndPreservesCause(t *testing.T) {
	sup := New(testSupervisorDeps())
	defer sup.Close()
	stream := subscribeForTest(t, sup)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := sup.StartResolved(ctx, testRuntimeDefinition(), session.MaintainAuthentication); !errors.Is(err, context.Canceled) {
		t.Fatal("original start cause lost", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	event := nextForTest(t, waitCtx, stream)
	if event.Kind != StateSessionRemoved || event.SessionID != "session-1" || event.Revision != 1 {
		t.Fatal("rollback omitted initial producer drain", event)
	}
}

func TestSupervisorFailedShutdownRetainsResourceAndOriginalCause(t *testing.T) {
	for _, operation := range []string{"remove", "forget", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			sup := New(testSupervisorDeps())
			defer sup.Close()
			stream := subscribeForTest(t, sup)
			id, _, err := sup.StartResolved(context.Background(), testRuntimeDefinition(), session.SuspendAuthentication)
			if err != nil {
				t.Fatal(err)
			}
			managed, err := sup.managed(id)
			if err != nil {
				t.Fatal(err)
			}
			if err := managed.actor.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("original start cause")
			switch operation {
			case "remove":
				err = sup.Remove(context.Background(), id)
			case "forget":
				err = sup.ForgetStopped(id)
			case "rollback":
				err = sup.rollbackNewSession(id, managed, cause)
			}
			if !errors.Is(err, session.ErrAuthenticationSessionClosed) {
				t.Fatal("shutdown cause lost", err)
			}
			if operation == "rollback" && !errors.Is(err, cause) {
				t.Fatal("start cause lost", err)
			}
			if retained, err := sup.managed(id); err != nil || retained != managed {
				t.Fatal("failed shutdown deleted entry", err)
			}
			select {
			case <-managed.stopForwarding:
				t.Fatal("failed shutdown stopped forwarder")
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			for {
				event, err := stream.Next(ctx)
				if err != nil {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(err)
					}
					break
				}
				if event.Kind == StateSessionRemoved {
					t.Fatal("failed shutdown published false terminal", event)
				}
			}
		})
	}
}

func TestSupervisorRollbackFailureRetainsActorDuringRunShutdown(t *testing.T) {
	run := newHeldCancellationRun()
	sup := New(testSupervisorDeps())
	defer func() { releaseHeldCancellationRun(run); sup.Close(); sup.Wait() }()
	stream := subscribeForTest(t, sup)
	definition := testRuntimeDefinition()
	definition.AuthenticationProtocolFactory = heldCancellationFactory{run: run}
	if err := sup.ApplySystemNetworkSnapshot(context.Background(), testNetworkSnapshot(1)); err != nil {
		t.Fatal(err)
	}
	id, _, err := sup.StartResolved(context.Background(), definition, session.MaintainAuthentication)
	if err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, run.started, "Run started")
	managed, _ := sup.managed(id)
	shutdown := make(chan error, 1)
	go func() { shutdown <- managed.actor.Shutdown(context.Background()) }()
	waitForSignal(t, run.canceled, "actor shutdown still awaits Run cleanup")
	cause := errors.New("failed start while actor shutdown is pending")
	err = sup.rollbackNewSession(id, managed, cause)
	if !errors.Is(err, cause) || !errors.Is(err, session.ErrAuthenticationSessionClosed) {
		t.Fatal(err)
	}
	if retained, err := sup.managed(id); err != nil || retained != managed {
		t.Fatal("still-running actor deleted", err)
	}
	select {
	case <-managed.stopForwarding:
		t.Fatal("failed shutdown stopped forwarding")
	default:
	}
	select {
	case <-shutdown:
		t.Fatal("test actor was already stopped")
	default:
	}
	// Whole-Supervisor Close wakes subscribers before blocked actor cleanup.
	closed := make(chan error, 1)
	go func() { closed <- sup.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, err := stream.Next(ctx)
		if errors.Is(err, ErrSubscriptionShutdown) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	releaseHeldCancellationRun(run)
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
