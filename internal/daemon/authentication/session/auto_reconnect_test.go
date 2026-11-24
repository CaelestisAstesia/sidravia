package session

import (
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

func newAutoReconnectDisabledSession(t *testing.T) (*AuthenticationSession, *controlledFactory, *manualRetryScheduler) {
	t.Helper()
	now := time.Unix(100, 0)
	policy := standardRetryPolicy()
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	definition.AutoReconnect = false
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, Dependencies{
		Now:            func() time.Time { return now },
		RetryPolicy:    policy,
		RetryScheduler: scheduler,
	})
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	return session, factory, scheduler
}

func TestSessionAutoReconnectFalseBlocksOnRetryableFailure(t *testing.T) {
	for _, tc := range []struct {
		name           string
		code           protocol.AuthenticationProtocolFailureCode
		description    string
		recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation
	}{
		{"standard", "network_timeout", "Network operation timed out.", protocol.RetryAfterStandardDelay},
		{"extended", "server_busy", "The authentication server reported busy.", protocol.RetryAfterExtendedDelay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, factory, scheduler := newAutoReconnectDisabledSession(t)
			defer shutdownTestSession(t, session)

			ctx := testContext(t)
			_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
			failure := &protocol.AuthenticationProtocolRunFailure{
				Code:                   tc.code,
				Description:            tc.description,
				HandlingRecommendation: tc.recommendation,
			}
			factory.run(0).unblock(failure)

			snapshot := waitForState(t, ctx, session, BlockedByError)
			if snapshot.StateReason == nil || snapshot.StateReason.Code != StateReasonCodeAutomaticReconnectDisabled {
				t.Fatalf("state reason = %#v, want %s", snapshot.StateReason, StateReasonCodeAutomaticReconnectDisabled)
			}
			if snapshot.LastAuthenticationFailure == nil ||
				snapshot.LastAuthenticationFailure.Code != tc.code ||
				snapshot.LastAuthenticationFailure.Description != tc.description ||
				snapshot.LastAuthenticationFailure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
				t.Fatalf("public failure = %#v", snapshot.LastAuthenticationFailure)
			}
			if failure.HandlingRecommendation != tc.recommendation {
				t.Fatalf("protocol failure recommendation = %q, want %q", failure.HandlingRecommendation, tc.recommendation)
			}
			if snapshot.NextRetryAt != nil {
				t.Fatalf("NextRetryAt = %v, want nil", snapshot.NextRetryAt)
			}
			if scheduler.count() != 0 {
				t.Fatalf("scheduler count = %d, want 0", scheduler.count())
			}
		})
	}
}

func TestSessionAutoReconnectFalseDoesNotRestartOnLaterSnapshot(t *testing.T) {
	session, factory, _ := newAutoReconnectDisabledSession(t)
	defer shutdownTestSession(t, session)

	ctx := testContext(t)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, BlockedByError)

	// A later network snapshot must not start a new run while blocked.
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Ethernet Updated"))
	if got := len(factory.creationInputs()); got != 1 {
		t.Fatalf("factory creation count after later snapshot = %d, want 1", got)
	}
	snapshot := sessionSnapshot(t, ctx, session)
	if snapshot.State != BlockedByError {
		t.Fatalf("state = %v, want %v", snapshot.State, BlockedByError)
	}
}

func TestSessionAutoReconnectFalseRestartClearsBlock(t *testing.T) {
	session, factory, _ := newAutoReconnectDisabledSession(t)
	defer shutdownTestSession(t, session)

	ctx := testContext(t)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, BlockedByError)

	if _, err := session.Restart(ctx); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	// After restart, a new run should be created.
	run := waitForFactoryRun(t, ctx, factory, 1)
	run.establish(ctx)
	if got := waitForState(t, ctx, session, Authenticated); got.State != Authenticated {
		t.Fatalf("state after restart = %v, want %v", got.State, Authenticated)
	}
}

func TestSessionAutoReconnectFalseRestartSurvivesNetworkCleanup(t *testing.T) {
	ctx := testContext(t)
	factory, session := newExplicitAutoReconnectDisabledSession(t)
	defer shutdownTestSession(t, session)

	_, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "target", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	oldRun := waitForFactoryRun(t, ctx, factory, 0)
	defer oldRun.unblock(nil)
	if err := oldRun.establish(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, ctx, session, Authenticated)

	_, err = session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil))
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupRequirement(t, oldRun.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
	_, err = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 3, "target", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(factory.creationInputs()); got != 1 {
		t.Fatalf("factory creation count during old cleanup = %d, want 1", got)
	}
	if _, err := session.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	oldRun.unblock(nil)

	replacement := waitForFactoryRun(t, ctx, factory, 1)
	defer replacement.unblock(nil)
	if got := len(factory.creationInputs()); got != 2 {
		t.Fatalf("factory creation count after repeated Restart = %d, want 2", got)
	}
	if err := replacement.establish(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, ctx, session, Authenticated)

	_, err = session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(4, time.Unix(4, 0), nil))
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupRequirement(t, replacement.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
	_, err = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 5, "target", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(factory.creationInputs()); got != 2 {
		t.Fatalf("factory creation count while replacement cleanup is held = %d, want 2", got)
	}
	replacement.unblock(nil)
	blocked := waitForState(t, ctx, session, BlockedByError)
	if blocked.StateReason == nil || blocked.StateReason.Code != StateReasonCodeAutomaticReconnectDisabled {
		t.Fatalf("later network loss state reason = %#v, want automatic reconnect disabled", blocked.StateReason)
	}
	if got := len(factory.creationInputs()); got != 2 {
		t.Fatalf("factory creation count after later loss = %d, want 2", got)
	}
}

func TestSessionAutoReconnectFalseRestartWaitsForBindingAfterCleanup(t *testing.T) {
	ctx := testContext(t)
	factory, session := newExplicitAutoReconnectDisabledSession(t)
	defer shutdownTestSession(t, session)

	_, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "target", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	oldRun := waitForFactoryRun(t, ctx, factory, 0)
	defer oldRun.unblock(nil)
	if err := oldRun.establish(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, ctx, session, Authenticated)
	_, err = session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil))
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupRequirement(t, oldRun.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
	if _, err := session.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	oldRun.unblock(nil)
	if got := len(factory.creationInputs()); got != 1 {
		t.Fatalf("factory creation count while binding is unavailable = %d, want 1", got)
	}

	_, err = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 3, "target", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := waitForFactoryRun(t, ctx, factory, 1)
	defer replacement.unblock(nil)
	if got := len(factory.creationInputs()); got != 2 {
		t.Fatalf("factory creation count after binding returns = %d, want 2", got)
	}
	if err := replacement.establish(ctx); err != nil {
		t.Fatal(err)
	}
	if got := waitForState(t, ctx, session, Authenticated); got.State != Authenticated {
		t.Fatalf("state after explicit restart = %v, want %v", got.State, Authenticated)
	}
}

func TestSessionAutoReconnectFalseSuspendOverridesPendingRestart(t *testing.T) {
	ctx := testContext(t)
	factory, session := newExplicitAutoReconnectDisabledSession(t)
	defer shutdownTestSession(t, session)

	_, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "target", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	oldRun := waitForFactoryRun(t, ctx, factory, 0)
	defer oldRun.unblock(nil)
	if err := oldRun.establish(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, ctx, session, Authenticated)
	_, err = session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil))
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupRequirement(t, oldRun.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
	if _, err := session.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stopping.State != Stopping || stopping.Intent != SuspendAuthentication {
		t.Fatalf("suspend after queued Restart = %#v, want stopping with suspend intent", stopping)
	}
	oldRun.unblock(nil)
	suspended := waitForState(t, ctx, session, Suspended)
	if suspended.Intent != SuspendAuthentication {
		t.Fatalf("final intent = %v, want suspend", suspended.Intent)
	}
	if got := len(factory.creationInputs()); got != 1 {
		t.Fatalf("factory creation count after suspend = %d, want 1", got)
	}
}

func newExplicitAutoReconnectDisabledSession(t *testing.T) (*controlledFactory, *AuthenticationSession) {
	t.Helper()
	factory := &controlledFactory{holdCancellation: true}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	definition.AutoReconnect = false
	definition.Configuration.NetworkBindingPolicy = NetworkBindingPolicy{
		Mode:             ExplicitInterfaceAndLocalIPv4,
		InterfaceID:      "target",
		LocalIPv4Address: netip.MustParseAddr("192.0.2.10"),
	}
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	return factory, session
}

func TestSessionAutoReconnectFalseSuspendReachesSuspended(t *testing.T) {
	session, factory, _ := newAutoReconnectDisabledSession(t)
	defer shutdownTestSession(t, session)

	ctx := testContext(t)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{HandlingRecommendation: protocol.RetryAfterStandardDelay})
	_ = waitForState(t, ctx, session, BlockedByError)

	if _, err := session.Suspend(ctx); err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	suspended := waitForState(t, ctx, session, Suspended)
	if suspended.State != Suspended {
		t.Fatalf("state = %v, want %v", suspended.State, Suspended)
	}
}
