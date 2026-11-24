package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/host"
)

type countingAutomaticLoginPerformer struct {
	mu    sync.Mutex
	count int
	err   error
}

func (p *countingAutomaticLoginPerformer) PerformAutomaticLogin(context.Context) error {
	p.mu.Lock()
	p.count++
	err := p.err
	p.mu.Unlock()
	return err
}

func (p *countingAutomaticLoginPerformer) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

func newLifecycleRuntimeWithAutomaticLogin(
	observer *fakeObserver,
	hostRunner *fakeHostRunner,
	sink *fakeSnapshotSink,
	shutdown *fakeShutdown,
	logger *slog.Logger,
	performer automaticLoginPerformer,
) *composedRuntime {
	return &composedRuntime{
		observer:       observer,
		snapshotSink:   sink,
		automaticLogin: performer,
		shutdown:       shutdown,
		hostRunner:     hostRunner.run,
		logger:         logger,
		productVersion: "test-version",
		buildID:        "test-build",
	}
}

func waitForSinkSnapshots(t *testing.T, sink *fakeSnapshotSink, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		sink.mu.Lock()
		got := len(sink.snapshots)
		sink.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %d snapshots, got %d", want, want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestDeliverSnapshotsInvokesAutomaticLoginOnceAfterFirstSnapshot(t *testing.T) {
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	performer := &countingAutomaticLoginPerformer{}
	shutdown := &fakeShutdown{activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done}}
	rt := newLifecycleRuntimeWithAutomaticLogin(observer, hostRunner, sink, shutdown, discardLogger(), performer)

	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	observer.addSnapshot(environment.NewSnapshot(2, time.Unix(101, 0), nil))

	ctx, cancel := context.WithCancel(context.Background())
	result := runRuntime(rt, ctx)

	waitForSinkSnapshots(t, sink, 2)
	if got := performer.Count(); got != 1 {
		t.Fatalf("automatic login count = %d, want 1", got)
	}
	cancel()
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("runtime error = %v", err)
	}
}

func TestDeliverSnapshotsAutomaticLoginFailureDoesNotTerminateDaemon(t *testing.T) {
	observer := newFakeObserver()
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	performer := &countingAutomaticLoginPerformer{err: errors.New("account-marker password-marker /private/path cause-marker")}
	shutdown := &fakeShutdown{activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done}}
	var logs bytes.Buffer
	rt := newLifecycleRuntimeWithAutomaticLogin(observer, hostRunner, sink, shutdown, bufferLogger(&logs), performer)

	ipc := &fakeIPCShutdown{called: make(chan struct{}), started: make(chan struct{})}
	rt.ipcShutdown = ipc

	observer.addSnapshot(environment.NewSnapshot(1, time.Unix(100, 0), nil))
	observer.addSnapshot(environment.NewSnapshot(2, time.Unix(101, 0), nil))

	ctx, cancel := context.WithCancel(context.Background())
	result := runRuntime(rt, ctx)

	waitForSinkSnapshots(t, sink, 2)

	// Verify the runtime is still running (not terminated by the failure).
	select {
	case err := <-result:
		t.Fatalf("runtime terminated after automatic login failure: %v", err)
	default:
	}

	if got := performer.Count(); got != 1 {
		t.Fatalf("automatic login count = %d, want 1", got)
	}
	select {
	case <-hostRunner.finished:
		t.Fatal("automatic login failure shut down Host")
	default:
	}
	select {
	case <-ipc.started:
		t.Fatal("automatic login failure shut down IPC")
	default:
	}
	cancel()
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("runtime error = %v", err)
	}
	waitForSignal(t, ipc.called, "IPC shutdown after explicit cancellation")
	output := logs.String()
	if strings.Count(output, "event="+eventAutomaticLoginFailed) != 1 || !strings.Contains(output, "msg="+msgAutomaticLoginFailed) {
		t.Fatal("automatic failure warning missing or repeated")
	}
	for _, secret := range []string{"account-marker", "password-marker", "/private/path", "cause-marker", "error="} {
		if strings.Contains(output, secret) {
			t.Fatal("automatic failure warning leaked cause")
		}
	}

}

func TestAutomaticLoginRequiresHostAdmission(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprintf("admitted-%v", success), func(t *testing.T) {
			observer := newFakeObserver()
			observer.addSnapshot(environment.NewSnapshot(1, time.Now(), nil))
			hostRunner := newFakeHostRunner()
			sink := newFakeSnapshotSink()
			performer := &countingAutomaticLoginPerformer{}
			shutdown := &fakeShutdown{activityDone: []<-chan struct{}{observer.done, hostRunner.finished}}
			rt := newLifecycleRuntimeWithAutomaticLogin(observer, hostRunner, sink, shutdown, discardLogger(), performer)
			release := make(chan struct{})
			rt.hostRunner = func(ctx context.Context, cfg host.Config) error {
				defer close(hostRunner.finished)
				close(hostRunner.started)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				if !success {
					return host.ErrAlreadyRunning
				}
				if cfg.OnReady != nil {
					cfg.OnReady()
				}
				<-ctx.Done()
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := runRuntime(rt, ctx)
			<-hostRunner.started
			waitForSinkSnapshots(t, sink, 1)
			// Selection cannot run between observing a network and owning the host.
			time.Sleep(10 * time.Millisecond)
			if performer.Count() != 0 {
				t.Error("automatic login occurred before host admission")
			}
			close(release)
			if success {
				deadline := time.Now().Add(time.Second)
				for performer.Count() == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if performer.Count() != 1 {
					t.Error("admitted host did not perform automatic login once")
				}
				cancel()
				if e := waitForRuntimeResult(t, done); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := waitForRuntimeResult(t, done); !errors.Is(e, host.ErrAlreadyRunning) {
					t.Fatal("lost host rejection cause")
				}
				if performer.Count() != 0 {
					t.Fatal("rejected host performed automatic login")
				}
			}
		})
	}
}
