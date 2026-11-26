package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"

	"sidravia/internal/daemon/internal/desktopowner"
	"sidravia/internal/launchcontract"
)

type fakeDesktopOwner struct {
	started    chan struct{}
	done       chan struct{}
	release    chan error
	closeCount int
	mu         sync.Mutex
}

func newFakeDesktopOwner() *fakeDesktopOwner {
	return &fakeDesktopOwner{started: make(chan struct{}), done: make(chan struct{}), release: make(chan error, 1)}
}
func (o *fakeDesktopOwner) Wait(ctx context.Context) error {
	close(o.started)
	defer close(o.done)
	select {
	case err := <-o.release:
		return err
	case <-ctx.Done():
		return nil
	}
}
func (o *fakeDesktopOwner) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closeCount++
	return nil
}

func TestDesktopOwnerExitUsesNormalBoundedShutdown(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	owner := newFakeDesktopOwner()
	rt.launchOptions, _ = launchcontract.Desktop(42)
	rt.desktopOwner = owner
	shutdown.activityDone = append(shutdown.activityDone, owner.done)
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "delivery start")
	waitForSignal(t, owner.started, "owner start")
	owner.release <- nil
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("owner exit error=%v", err)
	}
	assertShutdown(t, shutdown)
	owner.mu.Lock()
	closes := owner.closeCount
	owner.mu.Unlock()
	if closes != 1 {
		t.Fatalf("owner closes=%d", closes)
	}
}

func TestDesktopOwnerFailureIsRuntimeFailure(t *testing.T) {
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	owner := newFakeDesktopOwner()
	rt.launchOptions, _ = launchcontract.Desktop(42)
	rt.desktopOwner = owner
	shutdown.activityDone = append(shutdown.activityDone, owner.done)
	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "delivery start")
	cause := errors.New("owner failed")
	owner.release <- cause
	if err := waitForRuntimeResult(t, result); !errors.Is(err, cause) {
		t.Fatalf("err=%v", err)
	}
	assertShutdown(t, shutdown)
}

func TestDesktopOwnerJoinsExternalCancellationAndCommittedStop(t *testing.T) {
	for _, trigger := range []struct {
		name string
		fire func(context.CancelFunc, *composedRuntime)
	}{
		{"caller cancellation", func(cancel context.CancelFunc, _ *composedRuntime) { cancel() }},
		{"committed stop", func(_ context.CancelFunc, rt *composedRuntime) { rt.stopCh <- struct{}{} }},
	} {
		t.Run(trigger.name, func(t *testing.T) {
			rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
			owner := newFakeDesktopOwner()
			rt.launchOptions, _ = launchcontract.Desktop(42)
			rt.desktopOwner = owner
			rt.stopCh = make(chan struct{}, 1)
			shutdown.activityDone = append(shutdown.activityDone, owner.done)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := runRuntime(rt, ctx)
			waitForSignal(t, hostRunner.started, "host start")
			waitForSignal(t, observer.started, "observer start")
			waitForSignal(t, sink.started, "delivery start")
			waitForSignal(t, owner.started, "owner start")
			trigger.fire(cancel, rt)
			if err := waitForRuntimeResult(t, result); err != nil {
				t.Fatalf("run error=%v", err)
			}
			assertShutdown(t, shutdown)
			owner.mu.Lock()
			closes := owner.closeCount
			owner.mu.Unlock()
			if closes != 1 {
				t.Fatalf("owner closes=%d", closes)
			}
		})
	}
}

func TestCompositionLaunchOptionWatcherRequirements(t *testing.T) {
	paths := testPaths(t)
	writeTestProfile(t, paths.profiles, "jlu.json", validJLUProfile(t))
	options, _ := launchcontract.Desktop(42)
	if _, err := composeObjectGraphWithLaunchOptions(context.Background(), newInMemoryStore(), paths, testHostInfo(), newFakeObserver(), newFakeHostRunner().run, "token", "v", "b", discardLogger(), options, nil); err == nil {
		t.Fatal("desktop accepted no owner")
	}
	if _, err := composeObjectGraphWithLaunchOptions(context.Background(), newInMemoryStore(), paths, testHostInfo(), newFakeObserver(), newFakeHostRunner().run, "token", "v", "b", discardLogger(), launchcontract.Headless(), newFakeDesktopOwner()); err == nil {
		t.Fatal("headless accepted owner")
	}
}

var _ desktopowner.Watcher = (*fakeDesktopOwner)(nil)
