package environment

import (
	"context"
	"errors"
	"net/netip"
	"runtime"
	"testing"
	"time"
)

func TestSystemObserverPublishesFirstCollectionImmediatelyAsRevisionOne(t *testing.T) {
	calls := 0
	observer := &systemObserver{
		collect: func() ([]NetworkInterface, error) {
			calls++
			return cloneInterfaces([]NetworkInterface{testInterface("eth0")}), nil
		},
		now:      fixedClock(time.Unix(100, 0)),
		interval: time.Hour,
	}
	output := make(chan Snapshot, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- observer.Observe(ctx, output) }()

	select {
	case snapshot := <-output:
		if snapshot.Revision != 1 {
			t.Fatalf("expected revision 1, got %d", snapshot.Revision)
		}
		if snapshot.ObservedAt != time.Unix(100, 0) {
			t.Fatalf("expected observed at unix 100, got %v", snapshot.ObservedAt)
		}
		if got := len(snapshot.Interfaces()); got != 1 {
			t.Fatalf("expected 1 interface, got %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first collection was not published immediately")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("unexpected error after cancellation: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 collection before cancellation, got %d", calls)
	}
}

func TestSystemObserverSuppressesEquivalentAndPublishesRevisionTwo(t *testing.T) {
	base := testInterface("eth0")
	changed := testInterfaceWithGateway("eth0", netip.MustParseAddr("192.168.1.1"))

	calls := 0
	observer := &systemObserver{
		collect: func() ([]NetworkInterface, error) {
			calls++
			switch calls {
			case 1:
				return cloneInterfaces([]NetworkInterface{base}), nil
			case 2:
				return cloneInterfaces([]NetworkInterface{base}), nil
			default:
				return cloneInterfaces([]NetworkInterface{changed}), nil
			}
		},
		now:      stepClock(time.Unix(100, 0)),
		interval: 5 * time.Millisecond,
	}
	output := make(chan Snapshot, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- observer.Observe(ctx, output) }()

	first := receiveSnapshot(t, output)
	if first.Revision != 1 {
		t.Fatalf("expected first revision 1, got %d", first.Revision)
	}
	if first.ObservedAt != time.Unix(101, 0) {
		t.Fatalf("expected first observed at unix 101, got %v", first.ObservedAt)
	}

	second := receiveSnapshot(t, output)
	if second.Revision != 2 {
		t.Fatalf("expected second revision 2, got %d", second.Revision)
	}
	if second.ObservedAt != time.Unix(102, 0) {
		t.Fatalf("expected second observed at unix 102, got %v", second.ObservedAt)
	}
	if calls < 3 {
		t.Fatalf("expected at least 3 collections, got %d", calls)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("unexpected error after cancellation: %v", err)
	}
}

func TestSystemObserverSnapshotIsolatedFromCallerMutation(t *testing.T) {
	source := []NetworkInterface{testInterface("eth0")}
	observer := &systemObserver{
		collect: func() ([]NetworkInterface, error) {
			return cloneInterfaces(source), nil
		},
		now:      fixedClock(time.Unix(100, 0)),
		interval: time.Hour,
	}
	output := make(chan Snapshot, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- observer.Observe(ctx, output) }()

	snapshot := receiveSnapshot(t, output)

	// Mutate the source slice and the slice returned by the snapshot.
	source[0] = testInterface("changed")
	returnedInterfaces := snapshot.Interfaces()
	returnedInterfaces[0] = testInterface("mutated")

	cancel()
	<-done

	if got := string(snapshot.Interfaces()[0].InterfaceID); got != "eth0" {
		t.Fatalf("published snapshot was mutated through source or returned slice: %q", got)
	}
}

func TestSystemObserverBlockedSendExitsAfterCancellation(t *testing.T) {
	observer := &systemObserver{
		collect: func() ([]NetworkInterface, error) {
			return cloneInterfaces([]NetworkInterface{testInterface("eth0")}), nil
		},
		now:      fixedClock(time.Unix(100, 0)),
		interval: time.Hour,
	}
	output := make(chan Snapshot) // unbuffered, no receiver
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	before := runtime.NumGoroutine()
	go func() { done <- observer.Observe(ctx, output) }()

	// Let Observe collect and block on the unbuffered send.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil after cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked send did not exit after cancellation")
	}

	// Observe must not leave a helper goroutine behind.
	time.Sleep(50 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("observer left goroutines behind: before=%d after=%d", before, after)
	}
}

func TestSystemObserverCancellationDuringTimerWaitReturnsNil(t *testing.T) {
	observer := &systemObserver{
		collect: func() ([]NetworkInterface, error) {
			return cloneInterfaces([]NetworkInterface{testInterface("eth0")}), nil
		},
		now:      fixedClock(time.Unix(100, 0)),
		interval: time.Hour,
	}
	output := make(chan Snapshot, 1)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- observer.Observe(ctx, output) }()

	// First collection publishes immediately; Observe then waits on the
	// hour-long ticker.
	receiveSnapshot(t, output)
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil during timer wait, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timer wait did not exit after cancellation")
	}
}

func TestSystemObserverCollectorErrorPreservesCause(t *testing.T) {
	sentinel := errors.New("collect failed")
	observer := &systemObserver{
		collect: func() ([]NetworkInterface, error) {
			return nil, sentinel
		},
		now:      fixedClock(time.Unix(100, 0)),
		interval: time.Hour,
	}
	output := make(chan Snapshot, 1)

	err := observer.Observe(context.Background(), output)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected error to wrap sentinel cause, got %v", err)
	}
	select {
	case snapshot := <-output:
		t.Fatalf("collector error published a snapshot: %+v", snapshot)
	default:
	}
}

func TestSystemObserverRejectsNilContextAndOutput(t *testing.T) {
	observer := &systemObserver{
		collect:  func() ([]NetworkInterface, error) { return nil, nil },
		now:      fixedClock(time.Unix(100, 0)),
		interval: time.Hour,
	}
	if err := observer.Observe(nil, make(chan Snapshot, 1)); err == nil {
		t.Fatal("expected error for nil context")
	}
	if err := observer.Observe(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil output channel")
	}
}

func testInterface(id string) NetworkInterface {
	ifc, err := NewNetworkInterface(NetworkInterfaceFacts{
		InterfaceID:      InterfaceID(id),
		DisplayName:      id,
		OperationalState: OperationalStateUp,
		PhysicalMedium:   PhysicalMediumWired,
		HardwareAddress:  []byte{0, 1, 2, 3, 4, 5},
	})
	if err != nil {
		panic(err)
	}
	return ifc
}

func testInterfaceWithGateway(id string, gateway netip.Addr) NetworkInterface {
	ifc, err := NewNetworkInterface(NetworkInterfaceFacts{
		InterfaceID:                 InterfaceID(id),
		DisplayName:                 id,
		OperationalState:            OperationalStateUp,
		PhysicalMedium:              PhysicalMediumWired,
		HardwareAddress:             []byte{0, 1, 2, 3, 4, 5},
		DefaultIPv4GatewayAddresses: []netip.Addr{gateway},
	})
	if err != nil {
		panic(err)
	}
	return ifc
}

func cloneInterfaces(interfaces []NetworkInterface) []NetworkInterface {
	cloned := make([]NetworkInterface, len(interfaces))
	for index, value := range interfaces {
		cloned[index] = value.clone()
	}
	return cloned
}

func fixedClock(moment time.Time) nowClock {
	return func() time.Time { return moment }
}

func stepClock(start time.Time) nowClock {
	var step int
	return func() time.Time {
		step++
		return start.Add(time.Duration(step) * time.Second)
	}
}

func receiveSnapshot(t *testing.T, output <-chan Snapshot) Snapshot {
	t.Helper()
	select {
	case snapshot := <-output:
		return snapshot
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for snapshot")
		return Snapshot{}
	}
}
