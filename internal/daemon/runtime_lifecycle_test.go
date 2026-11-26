package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sidravia/internal/connection/environment"
)

func TestRuntimeLifecycleHostReturnsNilAfterOrderedSnapshotDelivery(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	hostRunner.signalDone(nil)

	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("run() error = %v, want nil on normal host shutdown", err)
	}
	waitForSignal(t, hostRunner.finished, "host completion")
	waitForSignal(t, observer.done, "observer completion")
	waitForSignal(t, sink.done, "delivery completion")
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleHostFailureCancelsOthers(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	hostErr := errors.New("host failure sentinel")
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	hostRunner.signalDone(hostErr)

	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, hostErr) {
		t.Fatalf("run() error = %v, want host sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleObserverFailurePreservesCause(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observerErr := errors.New("observer failure sentinel")
	observer.block = false
	observer.err = observerErr
	observer.afterPublish = sink.started

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, observerErr) {
		t.Fatalf("run() error = %v, want observer sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleObserverNilWhileActiveFails(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observer.block = false
	observer.afterPublish = sink.started

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if err == nil {
		t.Fatal("run() expected error when observer stopped while context remained active")
	}
	if !strings.Contains(err.Error(), "observer stopped unexpectedly") {
		t.Fatalf("run() error = %q, want unexpected observer stop", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleInvalidCallerContextsStartNothingAndShutDown(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, test := range []struct {
		name      string
		ctx       context.Context
		wantError bool
	}{
		{name: "nil", ctx: nil, wantError: true},
		{name: "already canceled", ctx: canceledCtx},
	} {
		t.Run(test.name, func(t *testing.T) {
			observer := newFakeObserver()
			hostRunner := newFakeHostRunner()
			sink := newFakeSnapshotSink()
			shutdown := &fakeShutdown{}
			rt := newLifecycleRuntime(observer, hostRunner, sink, shutdown, discardLogger())

			err := rt.run(test.ctx)
			if test.wantError && err == nil {
				t.Fatal("run() expected a nil-context error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("run() error = %v, want nil", err)
			}
			assertSignalOpen(t, hostRunner.started, "host")
			assertSignalOpen(t, observer.started, "observer")
			assertSignalOpen(t, sink.started, "snapshot delivery")
			assertShutdown(t, shutdown)
		})
	}
}

func TestRuntimeLifecycleExternalCancellationIsAlwaysNormal(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observer.returnContextError = true
	ctx, cancel := context.WithCancel(context.Background())

	result := runRuntime(rt, ctx)
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	cancel()

	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("run() error = %v, want nil after external cancellation", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleExternalCancellationPreservesNonCancellationFailure(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	observerErr := errors.New("observer failure concurrent with caller cancellation")
	observer.err = observerErr
	ctx, cancel := context.WithCancel(context.Background())

	result := runRuntime(rt, ctx)
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "snapshot delivery")
	cancel()

	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, observerErr) {
		t.Errorf("run() error = %v, want concurrent observer sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleSnapshotDeliveryFailurePreservesCause(t *testing.T) {
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	deliveryErr := errors.New("snapshot delivery failure sentinel")
	sink.err = deliveryErr
	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	shutdown := &fakeShutdown{
		activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done},
	}
	rt := newLifecycleRuntime(observer, hostRunner, sink, shutdown, discardLogger())

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, deliveryErr) {
		t.Fatalf("run() error = %v, want delivery sentinel in error chain", err)
	}
	assertShutdown(t, shutdown)
}

func TestRuntimeLifecycleShutdownCloseErrorIsReturnedAndJoined(t *testing.T) {
	closeErr := errors.New("shutdown close failure sentinel")

	t.Run("normal trigger", func(t *testing.T) {
		rt, _, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
		shutdown.closeErr = closeErr
		result := runRuntime(rt, context.Background())
		waitForSignal(t, hostRunner.started, "host start")
		waitForSignal(t, sink.started, "snapshot delivery")
		hostRunner.signalDone(nil)

		err := waitForRuntimeResult(t, result)
		if !errors.Is(err, closeErr) {
			t.Fatalf("run() error = %v, want close sentinel", err)
		}
		assertShutdown(t, shutdown)
	})

	t.Run("fatal trigger", func(t *testing.T) {
		rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
		observerErr := errors.New("observer failure joined with close")
		observer.block = false
		observer.err = observerErr
		observer.afterPublish = sink.started
		shutdown.closeErr = closeErr
		result := runRuntime(rt, context.Background())
		waitForSignal(t, hostRunner.started, "host start")

		err := waitForRuntimeResult(t, result)
		if !errors.Is(err, observerErr) || !errors.Is(err, closeErr) {
			t.Fatalf("run() error = %v, want observer and close sentinels", err)
		}
		assertShutdown(t, shutdown)
	})
}

func TestRuntimeLifecycleJoinsIPCShutdownFailure(t *testing.T) {
	rt, _, hostRunner, _, _ := newCoordinatedLifecycle(t, discardLogger())
	hostErr := errors.New("host failure sentinel")
	ipcErr := errors.New("ipc shutdown failure sentinel")
	ipc := &fakeIPCShutdown{err: ipcErr, called: make(chan struct{}), started: make(chan struct{})}
	rt.ipcShutdown = ipc

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	hostRunner.signalDone(hostErr)
	waitForSignal(t, ipc.started, "IPC shutdown start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, hostErr) || !errors.Is(err, ipcErr) {
		t.Fatalf("run() error = %v, want host and IPC shutdown sentinels", err)
	}
	waitForSignal(t, ipc.called, "IPC shutdown completion")
}
