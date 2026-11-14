package session

import (
	"context"
	"fmt"
	"sidravia/internal/daemon/authentication/protocol"
	"strings"
	"sync"
	"testing"
	"time"
)

type businessFactory struct{ *controlledFactory }

func (businessFactory) BusinessCapabilities(protocol.InstitutionProtocolConfiguration) (protocol.BusinessCapabilities, error) {
	return protocol.BusinessCapabilities{Statistics: true, TextNotifications: true}, nil
}
func newBusinessSession(t *testing.T) (*AuthenticationSession, *controlledFactory) {
	t.Helper()
	f := &controlledFactory{}
	d := validRuntimeDefinition(t)
	d.AuthenticationProtocolFactory = businessFactory{f}
	s, err := NewAuthenticationSession(d, MaintainAuthentication, testDependencies(time.Now))
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	t.Cleanup(func() { shutdownTestSession(t, s) })
	return s, f
}
func businessObserver(t *testing.T, ctx context.Context, r *controlledRun) protocol.AuthenticationProtocolRunObserver {
	t.Helper()
	select {
	case o := <-r.started:
		return o
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}
func TestBusinessSnapshotLifecycleAndOldRun(t *testing.T) {
	s, f := newBusinessSession(t)
	ctx := testContext(t)
	initial, err := s.Business(ctx)
	if err != nil || !initial.StatisticsSupported || initial.Current || len(initial.Messages) != 0 {
		t.Fatalf("initial %#v %v", initial, err)
	}
	_, _ = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "eth", "Ethernet"))
	r := waitForFactoryRun(t, ctx, f, 0)
	o := businessObserver(t, ctx, r)
	sink := o.(protocol.BusinessObserver)
	o.AuthenticationEstablished()
	waitForState(t, ctx, s, Authenticated)
	sink.ObserveAccountStatistics(protocol.AccountStatistics{HasTraffic: true, HasBalance: true})
	sink.ObserveTextNotification("  hello\r\nworld\t\x1b  ")
	value, err := s.Business(ctx)
	if err != nil || !value.Current || value.Statistics == nil || value.Statistics.TrafficBytes == nil || *value.Statistics.TrafficBytes != 0 || len(value.Messages) != 1 || value.Messages[0].Text != "hello\nworld" {
		t.Fatalf("value %#v %v", value, err)
	}
	revision := value.Revision
	sink.ObserveTextNotification("hello\nworld")
	value, _ = s.Business(ctx)
	if value.Revision != revision {
		t.Fatal("duplicate changed revision")
	}
	*value.Statistics.TrafficBytes = 99
	value.Messages[0].Text = "mutated"
	fresh, _ := s.Business(ctx)
	if *fresh.Statistics.TrafficBytes != 0 || fresh.Messages[0].Text == "mutated" {
		t.Fatal("snapshot aliases actor state")
	}
	for i := 0; i < 25; i++ {
		sink.ObserveTextNotification(fmt.Sprintf("bounded %d", i))
	}
	bounded, err := s.Business(ctx)
	if err != nil || len(bounded.Messages) != 16 || bounded.Messages[15].Text != "bounded 24" {
		t.Fatalf("bounded messages %#v %v", bounded, err)
	}
	_, err = s.Suspend(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, ctx, s, Suspended)
	sink.ObserveTextNotification("old canceled")
	stopped, _ := s.Business(ctx)
	if stopped.Current || len(stopped.Messages) != 16 {
		t.Fatalf("stopped %#v", stopped)
	}
	_, err = s.Restart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := waitForFactoryRun(t, ctx, f, 1)
	o2 := businessObserver(t, ctx, next)
	sink.ObserveTextNotification("old again")
	o2.AuthenticationEstablished()
	waitForState(t, ctx, s, Authenticated)
	fresh, _ = s.Business(ctx)
	if fresh.RunGeneration <= stopped.RunGeneration || fresh.Statistics != nil || len(fresh.Messages) != 0 || fresh.Revision <= stopped.Revision {
		t.Fatalf("new generation %#v", fresh)
	}
	o2.(protocol.BusinessObserver).ObserveTextNotification("new")
	fresh, _ = s.Business(ctx)
	if len(fresh.Messages) != 1 || fresh.Messages[0].Text != "new" {
		t.Fatal("new result absent")
	}
}
func TestBusinessFloodIsBoundedAndCancelable(t *testing.T) {
	s, f := newBusinessSession(t)
	ctx := testContext(t)
	_, _ = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "eth", "Ethernet"))
	o := businessObserver(t, ctx, waitForFactoryRun(t, ctx, f, 0))
	o.AuthenticationEstablished()
	waitForState(t, ctx, s, Authenticated)
	sink := o.(protocol.BusinessObserver)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20000; i++ {
			sink.ObserveTextNotification(fmt.Sprintf("notification %d", i))
			sink.ObserveAccountStatistics(protocol.AccountStatistics{HasTraffic: true, TrafficBytes: uint64(i)})
		}
	}()
	if _, err := s.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	waitForState(t, ctx, s, Suspended)
	v, err := s.Business(ctx)
	if err != nil || v.Current || len(v.Messages) > 16 {
		t.Fatalf("flood %#v %v", v, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Business(canceled); err == nil {
		t.Fatal("canceled query succeeded")
	}
}
func TestBusinessNormalizationAndUnsupported(t *testing.T) {
	for _, text := range []string{"", string([]byte{255}), strings.Repeat("界", 171), "\u202e\x1b"} {
		if _, ok := normalizeBusinessText(text); ok {
			t.Fatalf("invalid normalized %q", text)
		}
	}
	if s, ok := normalizeBusinessText(strings.Repeat("界", 170)); !ok || len(s) != 510 {
		t.Fatal("unicode boundary")
	}
	s := newTestSession(t, &controlledFactory{}, MaintainAuthentication)
	defer shutdownTestSession(t, s)
	v, err := s.Business(testContext(t))
	if err != nil || v.StatisticsSupported || v.TextSupported {
		t.Fatal("optional capability changed base protocol")
	}
}
