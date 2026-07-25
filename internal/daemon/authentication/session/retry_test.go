package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func TestSessionSchedulesStandardAndExtendedRetry(t *testing.T) {
	now := time.Unix(100, 0)
	policy := &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
		protocol.RetryAfterStandardDelay: 5 * time.Second,
		protocol.RetryAfterExtendedDelay: 30 * time.Second,
	}}
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return now })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	first := waitForState(t, ctx, session, WaitingBeforeRetry)
	if first.NextRetryAt == nil || !first.NextRetryAt.Equal(now.Add(5*time.Second)) {
		t.Fatalf("standard retry deadline = %v, want %v", first.NextRetryAt, now.Add(5*time.Second))
	}
	if got := scheduler.delay(0); got != 5*time.Second {
		t.Fatalf("standard scheduler delay = %v, want %v", got, 5*time.Second)
	}
	if got := policy.calls(); len(got) != 1 || got[0].recommendation != protocol.RetryAfterStandardDelay || got[0].consecutiveFailures != 1 {
		t.Fatalf("standard retry policy calls = %#v", got)
	}

	scheduler.callback(0)()
	waitForFactoryRun(t, ctx, factory, 1).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterExtendedDelay})
	second := waitForState(t, ctx, session, WaitingBeforeRetry)
	if second.NextRetryAt == nil || !second.NextRetryAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("extended retry deadline = %v, want %v", second.NextRetryAt, now.Add(30*time.Second))
	}
	if got := scheduler.delay(1); got != 30*time.Second {
		t.Fatalf("extended scheduler delay = %v, want %v", got, 30*time.Second)
	}
	if got := policy.calls(); len(got) != 2 || got[1].recommendation != protocol.RetryAfterExtendedDelay || got[1].consecutiveFailures != 2 {
		t.Fatalf("extended retry policy calls = %#v", got)
	}
}

func TestSessionBlocksWithoutTimerForBlockingFailure(t *testing.T) {
	for _, test := range []struct {
		name               string
		recommendation     protocol.AuthenticationProtocolFailureHandlingRecommendation
		delays             map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration
		wantPolicyCalls    int
		wantRecommendation protocol.AuthenticationProtocolFailureHandlingRecommendation
	}{
		{
			name:               "blocking recommendation",
			recommendation:     protocol.BlockUntilExplicitRestartOrRelevantInputChange,
			delays:             map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{},
			wantRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange,
		},
		{
			name:               "unavailable retry delay",
			recommendation:     protocol.RetryAfterStandardDelay,
			delays:             map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{},
			wantPolicyCalls:    1,
			wantRecommendation: protocol.RetryAfterStandardDelay,
		},
		{
			name:               "unknown recommendation",
			recommendation:     "unknown",
			delays:             map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{},
			wantRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := &recordingRetryPolicy{delays: test.delays}
			scheduler := &manualRetryScheduler{}
			factory := &controlledFactory{}
			session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
			ctx := testContext(t)
			defer shutdownTestSession(t, session)

			_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
			factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: test.recommendation})
			got := waitForState(t, ctx, session, BlockedByError)
			if got.NextRetryAt != nil || len(policy.calls()) != test.wantPolicyCalls || scheduler.count() != 0 {
				t.Fatalf("blocking failure scheduled a retry: snapshot=%#v calls=%#v schedules=%d", got, policy.calls(), scheduler.count())
			}
			if got.LastAuthenticationFailure == nil || got.LastAuthenticationFailure.HandlingRecommendation != test.wantRecommendation {
				t.Fatalf("public failure recommendation = %#v, want %q", got.LastAuthenticationFailure, test.wantRecommendation)
			}
		})
	}
}

func TestSessionIgnoresStaleRetryTimer(t *testing.T) {
	policy := &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
		protocol.RetryAfterStandardDelay: time.Second,
	}}
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, WaitingBeforeRetry)
	staleCallback := scheduler.callback(0)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "wifi", "Wi-Fi"))
	waitForFactoryRun(t, ctx, factory, 1)

	staleCallback()
	_ = sessionSnapshot(t, ctx, session)
	if got := len(factory.creationInputs()); got != 2 {
		t.Fatalf("stale retry timer started %d runs, want 2", got)
	}
}

func TestSessionResetsFailureCountAfterAuthenticationEstablished(t *testing.T) {
	policy := &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
		protocol.RetryAfterStandardDelay: time.Second,
	}}
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, WaitingBeforeRetry)
	scheduler.callback(0)()
	secondRun := waitForFactoryRun(t, ctx, factory, 1)
	if err := secondRun.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	_ = waitForState(t, ctx, session, Authenticated)
	secondRun.unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, WaitingBeforeRetry)
	if got := policy.calls(); len(got) != 2 || got[1].consecutiveFailures != 1 {
		t.Fatalf("failure counts = %#v, want reset count 1 after authentication", got)
	}
}

func TestSessionClearsRetryOnRelevantNetworkChange(t *testing.T) {
	policy := &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
		protocol.RetryAfterStandardDelay: time.Second,
	}}
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, WaitingBeforeRetry)
	cancellation := scheduler.cancellation(0)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "wifi", "Wi-Fi"))
	got := waitForState(t, ctx, session, Authenticating)
	if !cancellation.cancelled() || got.NextRetryAt != nil {
		t.Fatalf("relevant network change did not clear retry: cancelled=%v snapshot=%#v", cancellation.cancelled(), got)
	}
	if calls := policy.calls(); len(calls) != 1 {
		t.Fatalf("relevant network change unexpectedly used retry policy: %#v", calls)
	}
}

func TestSessionKeepsRetryOnEquivalentNetworkSnapshot(t *testing.T) {
	now := time.Unix(100, 0)
	policy := &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
		protocol.RetryAfterStandardDelay: time.Second,
	}}
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return now })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	waiting := waitForState(t, ctx, session, WaitingBeforeRetry)
	if waiting.NextRetryAt == nil {
		t.Fatal("retry did not record a deadline")
	}
	wantRetryAt := *waiting.NextRetryAt
	cancellation := scheduler.cancellation(0)

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Renamed Ethernet"))
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	if got.State != WaitingBeforeRetry || got.NextRetryAt == nil || !got.NextRetryAt.Equal(wantRetryAt) {
		t.Fatalf("equivalent network snapshot changed retry state: %#v", got)
	}
	if cancellation.cancelled() || scheduler.count() != 1 || len(factory.creationInputs()) != 1 {
		t.Fatalf("equivalent network snapshot changed active retry: cancelled=%v schedules=%d runs=%d", cancellation.cancelled(), scheduler.count(), len(factory.creationInputs()))
	}
}

func TestSessionBlocksOnZeroOrNegativeRetryDelay(t *testing.T) {
	for _, delay := range []time.Duration{0, -time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			policy := &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
				protocol.RetryAfterStandardDelay: delay,
			}}
			scheduler := &manualRetryScheduler{}
			factory := &controlledFactory{}
			session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
			ctx := testContext(t)
			defer shutdownTestSession(t, session)

			_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
			factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
			got := waitForState(t, ctx, session, BlockedByError)
			if got.NextRetryAt != nil || scheduler.count() != 0 || len(policy.calls()) != 1 {
				t.Fatalf("invalid delay %v scheduled retry: snapshot=%#v schedules=%d calls=%#v", delay, got, scheduler.count(), policy.calls())
			}
		})
	}
}

func TestSessionCancelsRetryOnSuspendRestartReplacementAndShutdown(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(context.Context, *AuthenticationSession, *controlledFactory) (Snapshot, error)
	}{
		{
			name: "suspend",
			apply: func(ctx context.Context, session *AuthenticationSession, _ *controlledFactory) (Snapshot, error) {
				return session.Suspend(ctx)
			},
		},
		{
			name: "restart",
			apply: func(ctx context.Context, session *AuthenticationSession, _ *controlledFactory) (Snapshot, error) {
				return session.Restart(ctx)
			},
		},
		{
			name: "replacement",
			apply: func(ctx context.Context, session *AuthenticationSession, factory *controlledFactory) (Snapshot, error) {
				definition := validRuntimeDefinition(t)
				definition.AuthenticationProtocolFactory = factory
				definition.Configuration.DisplayName = "Replacement"
				return session.ReplaceRuntimeDefinition(ctx, definition)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := standardRetryPolicy()
			scheduler := &manualRetryScheduler{}
			factory := &controlledFactory{}
			session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
			ctx := testContext(t)
			defer shutdownTestSession(t, session)

			scheduleStandardRetry(t, ctx, session, factory)
			cancellation := scheduler.cancellation(0)
			got, err := test.apply(ctx, session, factory)
			if err != nil {
				t.Fatalf("%s error = %v", test.name, err)
			}
			if !cancellation.cancelled() || cancellation.cancelCount() != 1 || got.NextRetryAt != nil {
				t.Fatalf("%s did not cancel retry once: cancelled=%v count=%d snapshot=%#v", test.name, cancellation.cancelled(), cancellation.cancelCount(), got)
			}
		})
	}

	t.Run("shutdown", func(t *testing.T) {
		policy := standardRetryPolicy()
		scheduler := &manualRetryScheduler{}
		factory := &controlledFactory{}
		session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
		ctx := testContext(t)

		scheduleStandardRetry(t, ctx, session, factory)
		cancellation := scheduler.cancellation(0)
		if err := session.Shutdown(ctx); err != nil {
			t.Fatalf("shutdown() error = %v", err)
		}
		if !cancellation.cancelled() || cancellation.cancelCount() != 1 {
			t.Fatalf("shutdown did not cancel retry once: cancelled=%v count=%d", cancellation.cancelled(), cancellation.cancelCount())
		}
	})
}

func TestSessionResetsFailureCountAfterRestartReplacementAndBindingChange(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(context.Context, *AuthenticationSession, *controlledFactory) error
	}{
		{
			name: "restart",
			apply: func(ctx context.Context, session *AuthenticationSession, _ *controlledFactory) error {
				_, err := session.Restart(ctx)
				return err
			},
		},
		{
			name: "replacement",
			apply: func(ctx context.Context, session *AuthenticationSession, factory *controlledFactory) error {
				definition := validRuntimeDefinition(t)
				definition.AuthenticationProtocolFactory = factory
				_, err := session.ReplaceRuntimeDefinition(ctx, definition)
				return err
			},
		},
		{
			name: "binding change",
			apply: func(ctx context.Context, session *AuthenticationSession, _ *controlledFactory) error {
				_, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "wifi", "Wi-Fi"))
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := standardRetryPolicy()
			scheduler := &manualRetryScheduler{}
			factory := &controlledFactory{}
			session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
			ctx := testContext(t)
			defer shutdownTestSession(t, session)

			scheduleStandardRetry(t, ctx, session, factory)
			if err := test.apply(ctx, session, factory); err != nil {
				t.Fatalf("%s error = %v", test.name, err)
			}
			waitForFactoryRun(t, ctx, factory, 1).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
			_ = waitForState(t, ctx, session, WaitingBeforeRetry)
			calls := policy.calls()
			if len(calls) != 2 || calls[1].consecutiveFailures != 1 {
				t.Fatalf("%s failure counts = %#v, want second count 1", test.name, calls)
			}
		})
	}
}

func TestSessionIgnoresRetryTimerWhenStateIsNotWaiting(t *testing.T) {
	policy := standardRetryPolicy()
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	scheduleStandardRetry(t, ctx, session, factory)
	scheduleID := session.retryScheduleID
	scheduler.callback(0)()
	got := waitForFactoryRun(t, ctx, factory, 1)
	if session.retryCancellation != nil || sessionSnapshot(t, ctx, session).NextRetryAt != nil {
		t.Fatal("timer expiry did not clear retry cancellation and deadline before starting a run")
	}

	session.post(authenticationRetryDelayElapsedEvent{scheduleID: scheduleID})
	_ = sessionSnapshot(t, ctx, session)
	if runs := len(factory.creationInputs()); runs != 2 {
		t.Fatalf("timer event outside waiting state started %d runs, want 2", runs)
	}
	got.unblock(nil)
}

func TestRetryCallbackAfterShutdownCannotMutateSession(t *testing.T) {
	policy := standardRetryPolicy()
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
	ctx := testContext(t)

	scheduleStandardRetry(t, ctx, session, factory)
	callback := scheduler.callback(0)
	if err := session.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
	callback()
	select {
	case <-session.done:
	default:
		t.Fatal("retry callback changed shutdown completion")
	}
}

func standardRetryPolicy() *recordingRetryPolicy {
	return &recordingRetryPolicy{delays: map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration{
		protocol.RetryAfterStandardDelay: time.Second,
	}}
}

func scheduleStandardRetry(t *testing.T, ctx context.Context, session *AuthenticationSession, factory *controlledFactory) {
	t.Helper()
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, WaitingBeforeRetry)
}

func newRetryTestSession(t *testing.T, factory *controlledFactory, policy *recordingRetryPolicy, scheduler *manualRetryScheduler, now func() time.Time) *AuthenticationSession {
	t.Helper()
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, Dependencies{
		Now:            now,
		RetryPolicy:    policy,
		RetryScheduler: scheduler,
	})
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	return session
}

type retryPolicyCall struct {
	recommendation      protocol.AuthenticationProtocolFailureHandlingRecommendation
	consecutiveFailures uint32
}

type recordingRetryPolicy struct {
	mu     sync.Mutex
	delays map[protocol.AuthenticationProtocolFailureHandlingRecommendation]time.Duration
	calls_ []retryPolicyCall
}

func (policy *recordingRetryPolicy) Delay(recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation, consecutiveFailures uint32) (time.Duration, bool) {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	policy.calls_ = append(policy.calls_, retryPolicyCall{recommendation: recommendation, consecutiveFailures: consecutiveFailures})
	delay, ok := policy.delays[recommendation]
	return delay, ok
}

func (policy *recordingRetryPolicy) calls() []retryPolicyCall {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	return append([]retryPolicyCall(nil), policy.calls_...)
}

type manualRetryScheduler struct {
	mu        sync.Mutex
	deadlines []time.Duration
	callbacks []func()
	cancels   []*manualRetryCancellation
}

func (scheduler *manualRetryScheduler) Schedule(delay time.Duration, callback func()) RetryCancellation {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	cancellation := &manualRetryCancellation{}
	scheduler.deadlines = append(scheduler.deadlines, delay)
	scheduler.callbacks = append(scheduler.callbacks, callback)
	scheduler.cancels = append(scheduler.cancels, cancellation)
	return cancellation
}

func (scheduler *manualRetryScheduler) count() int {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return len(scheduler.callbacks)
}

func (scheduler *manualRetryScheduler) callback(index int) func() {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.callbacks[index]
}

func (scheduler *manualRetryScheduler) delay(index int) time.Duration {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.deadlines[index]
}

func (scheduler *manualRetryScheduler) cancellation(index int) *manualRetryCancellation {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.cancels[index]
}

type manualRetryCancellation struct {
	mu           sync.Mutex
	cancelled_   bool
	cancelCount_ uint32
}

func (cancellation *manualRetryCancellation) Cancel() {
	cancellation.mu.Lock()
	defer cancellation.mu.Unlock()
	cancellation.cancelled_ = true
	cancellation.cancelCount_++
}

func (cancellation *manualRetryCancellation) cancelled() bool {
	cancellation.mu.Lock()
	defer cancellation.mu.Unlock()
	return cancellation.cancelled_
}

func (cancellation *manualRetryCancellation) cancelCount() uint32 {
	cancellation.mu.Lock()
	defer cancellation.mu.Unlock()
	return cancellation.cancelCount_
}
