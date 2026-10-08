package supervisor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/session"
	environment "sidravia/internal/daemon/environment"
)

func subscribeForTest(t *testing.T, sup *Supervisor) *Subscription {
	t.Helper()
	sub, err := sup.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	return sub
}

func nextForTest(t *testing.T, ctx context.Context, sub *Subscription) StateEvent {
	t.Helper()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func changedForTest(id ID, revision uint64) StateEvent {
	return StateEvent{Kind: StateSessionChanged, SessionID: id, Revision: revision, Snapshot: Snapshot{AuthenticationSessionID: id, Revision: revision}}
}

func subscriptionOwnedForTest(sup *Supervisor, sub *Subscription) bool {
	sup.mu.Lock()
	defer sup.mu.Unlock()
	_, owned := sup.subs[sub]
	return owned
}

func TestSubscriptionNextCancellationRetainsOwnership(t *testing.T) {
	sup := New(Dependencies{})
	defer sup.Close()
	sub := subscribeForTest(t, sup)
	cause := errors.New("consumer canceled current wait")
	ctx, cancel := context.WithCancelCause(context.Background())
	result := make(chan error, 1)
	go func() { _, err := sub.Next(ctx); result <- err }()
	cancel(cause)
	select {
	case err := <-result:
		if !errors.Is(err, cause) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not wake")
	}
	if !subscriptionOwnedForTest(sup, sub) {
		t.Fatal("per-call cancellation unregistered stream")
	}
	sup.publishStateEvent(changedForTest("after-cancel", 1))
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if event := nextForTest(t, ctx2, sub); event.SessionID != "after-cancel" {
		t.Fatal(event)
	}
	sub.Close()
	if subscriptionOwnedForTest(sup, sub) {
		t.Fatal("Close retained stream")
	}
}

func TestSubscriptionCloseAndShutdownWakeAllWaiters(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			sup := New(Dependencies{})
			defer sup.Close()
			sub := subscribeForTest(t, sup)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			results := make(chan error, 4)
			for range 4 {
				go func() { _, err := sub.Next(ctx); results <- err }()
			}
			want := ErrSubscriptionClosed
			if shutdown {
				want = ErrSubscriptionShutdown
				if err := sup.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				var wg sync.WaitGroup
				for range 4 {
					wg.Add(1)
					go func() { defer wg.Done(); sub.Close() }()
				}
				wg.Wait()
			}
			for range 4 {
				select {
				case err := <-results:
					var typed StateStreamError
					if !errors.Is(err, want) || !errors.As(err, &typed) {
						t.Fatalf("termination=%v", err)
					}
				case <-ctx.Done():
					t.Fatal("waiter not woken")
				}
			}
			if subscriptionOwnedForTest(sup, sub) {
				t.Fatal("ended stream still registered")
			}
			sub.Close()
			if _, err := sub.Next(ctx); !errors.Is(err, want) {
				t.Fatalf("idempotent close changed cause: %v", err)
			}
			if shutdown {
				if _, err := sup.Subscribe(); !errors.Is(err, ErrSubscriptionShutdown) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSubscriptionFIFOCoalescesWithoutMovingResources(t *testing.T) {
	sup := New(Dependencies{})
	defer sup.Close()
	sub := subscribeForTest(t, sup)
	sup.publishStateEvent(changedForTest("A", 1))
	sup.publishStateEvent(StateEvent{Kind: StateNetworkChanged, NetworkSnapshot: testNetworkSnapshot(1)})
	sup.publishStateEvent(changedForTest("B", 1))
	sup.publishStateEvent(changedForTest("A", 3))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a, network, b := nextForTest(t, ctx, sub), nextForTest(t, ctx, sub), nextForTest(t, ctx, sub)
	if a.SessionID != "A" || a.Revision != 3 || network.Kind != StateNetworkChanged || b.SessionID != "B" {
		t.Fatalf("FIFO=%#v %#v %#v", a, network, b)
	}
}

func TestSubscriptionCapacityOverflowIsOwnedDiscontinuity(t *testing.T) {
	sup := New(Dependencies{})
	defer sup.Close()
	slow, fast := subscribeForTest(t, sup), subscribeForTest(t, sup)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < subscriptionCapacity; i++ {
		event := changedForTest(ID(fmt.Sprintf("resource-%d", i)), 1)
		sup.publishStateEvent(event)
		if got := nextForTest(t, ctx, fast); got.SessionID != event.SessionID {
			t.Fatal(got)
		}
	}
	// Every replacement fits at full capacity and retains its FIFO position.
	for i := 0; i < subscriptionCapacity; i++ {
		event := changedForTest(ID(fmt.Sprintf("resource-%d", i)), 2)
		sup.publishStateEvent(event)
		nextForTest(t, ctx, fast)
	}
	if !subscriptionOwnedForTest(sup, slow) {
		t.Fatal("replacement consumed capacity")
	}
	sup.publishStateEvent(StateEvent{Kind: StateNetworkChanged, NetworkSnapshot: testNetworkSnapshot(1)})
	if _, err := slow.Next(ctx); !errors.Is(err, ErrSubscriptionOverflow) {
		t.Fatalf("overflow silently evicted: %v", err)
	}
	if subscriptionOwnedForTest(sup, slow) || !subscriptionOwnedForTest(sup, fast) {
		t.Fatal("overflow ownership affected sibling")
	}
	if nextForTest(t, ctx, fast).Kind != StateNetworkChanged {
		t.Fatal("sibling lost event")
	}
	// An ended stream reports its cause immediately to every blocked caller.
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := slow.Next(ctx); results <- err }()
	}
	for range 2 {
		if err := <-results; !errors.Is(err, ErrSubscriptionOverflow) {
			t.Fatal(err)
		}
	}
}

func TestSubscriptionFullQueueReturnsAllResourcesWithoutEviction(t *testing.T) {
	sup := New(Dependencies{})
	defer sup.Close()
	sub := subscribeForTest(t, sup)
	for i := 0; i < subscriptionCapacity; i++ {
		sup.publishStateEvent(changedForTest(ID(fmt.Sprint(i)), 1))
	}
	sup.publishStateEvent(changedForTest("0", 7))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < subscriptionCapacity; i++ {
		event := nextForTest(t, ctx, sub)
		want := uint64(1)
		if i == 0 {
			want = 7
		}
		if event.SessionID != ID(fmt.Sprint(i)) || event.Revision != want {
			t.Fatal("resource evicted or moved", event)
		}
	}
}

func TestSubscriptionTerminalPrecedenceAtSameRevision(t *testing.T) {
	sup := New(Dependencies{})
	defer sup.Close()
	sub := subscribeForTest(t, sup)
	sup.publishStateEvent(changedForTest("terminal", 8))
	sup.publishStateEvent(StateEvent{Kind: StateSessionRemoved, SessionID: "terminal", Revision: 8})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	event := nextForTest(t, ctx, sub)
	if event.Kind != StateSessionRemoved || event.Revision != 8 || event.Snapshot != (Snapshot{}) {
		t.Fatal(event)
	}
}

func TestSubscriptionNetworkAndSessionClones(t *testing.T) {
	sup := New(Dependencies{})
	defer sup.Close()
	left, right := subscribeForTest(t, sup), subscribeForTest(t, sup)
	event := changedForTest("nested", 1)
	event.Snapshot.StateReason = &session.StateReason{Description: "original"}
	sup.publishStateEvent(event)
	event.Snapshot.StateReason.Description = "source changed"
	network := testNetworkSnapshot(4)
	sup.publishStateEvent(StateEvent{Kind: StateNetworkChanged, NetworkSnapshot: network})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a, b := nextForTest(t, ctx, left), nextForTest(t, ctx, right)
	a.Snapshot.StateReason.Description = "consumer changed"
	if b.Snapshot.StateReason.Description != "original" {
		t.Fatal("nested Snapshot aliased")
	}
	x, y := nextForTest(t, ctx, left), nextForTest(t, ctx, right)
	x.NetworkSnapshot.Revision = 99
	interfaces := x.NetworkSnapshot.Interfaces()
	interfaces[0].InterfaceID = "changed"
	assignments := interfaces[0].IPv4AddressAssignments()
	assignments[0].PrefixLength = 1
	if !reflect.DeepEqual(y.NetworkSnapshot, network) {
		t.Fatal("network event aliased source or sibling")
	}
}

func TestSubscriptionRejectsInvalidEventShapeWithDiscontinuity(t *testing.T) {
	for _, event := range []StateEvent{
		{Kind: StateSessionChanged, SessionID: "mismatch", Revision: 1, Snapshot: Snapshot{AuthenticationSessionID: "mismatch", Revision: 2}},
		{Kind: StateSessionRemoved, SessionID: "zero"},
		{Kind: StateSessionRemoved, SessionID: "data", Revision: 1, Snapshot: Snapshot{Revision: 1}},
		{Kind: StateNetworkChanged, SessionID: "extra", NetworkSnapshot: environment.NewSnapshot(1, time.Time{}, nil)},
		{Kind: "unknown"},
	} {
		sup := New(Dependencies{})
		sub := subscribeForTest(t, sup)
		sup.publishStateEvent(event)
		if _, err := sub.Next(context.Background()); !errors.Is(err, ErrSubscriptionInvalidEvent) {
			t.Fatal("invalid event escaped", event, err)
		}
		if subscriptionOwnedForTest(sup, sub) {
			t.Fatal("invalid source retained stream")
		}
		sup.Close()
	}
}
