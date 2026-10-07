package session

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

func TestSessionMaintainingAuthenticationMovesFromWaitingToAuthenticated(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	if got := sessionSnapshot(t, ctx, session); got.State != WaitingForNetwork {
		t.Fatalf("initial state = %q, want %q", got.State, WaitingForNetwork)
	}
	network := usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet")
	if got, err := session.ApplySystemNetworkSnapshot(ctx, network); err != nil || got.State != Authenticating {
		t.Fatalf("applySystemNetworkSnapshot() = (%q, %v), want authenticating and nil", got.State, err)
	}
	inputs := factory.creationInputs()
	if len(inputs) != 1 {
		t.Fatalf("factory creation count = %d, want 1", len(inputs))
	}
	assertExactFactoryInputs(t, inputs[0], validRuntimeDefinition(t), network)

	if err := factory.run(0).establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	got := waitForState(t, ctx, session, Authenticated)
	if got.LastAuthenticationFailure != nil || got.AuthenticationEstablishedAt == nil {
		t.Fatalf("authenticated snapshot = %#v, want cleared failure and establishment time", got)
	}
}

func TestSessionDoesNotStartProtocolWhileSuspended(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, SuspendAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err != nil || got.State != Suspended {
		t.Fatalf("applySystemNetworkSnapshot() = (%q, %v), want suspended and nil", got.State, err)
	}
	if got := len(factory.creationInputs()); got != 0 {
		t.Fatalf("factory creation count = %d, want 0", got)
	}
}

func TestSessionIgnoresEstablishedEventFromStaleGeneration(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First"))
	first := factory.run(0)
	if err := first.waitForStart(ctx); err != nil {
		t.Fatalf("first run did not start: %v", err)
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second"))
	if cause := first.waitForCancellation(ctx); cause == nil {
		t.Fatal("first run was not cancelled")
	}
	if err := first.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	first.unblock(nil)
	second := waitForFactoryRun(t, ctx, factory, 1)
	got := sessionSnapshot(t, ctx, session)
	if got.State != Authenticating || got.SelectedNetworkBinding.InterfaceID != "second" {
		t.Fatalf("snapshot after stale establishment = %#v", got)
	}
	second.unblock(nil)
}

func TestSessionKeepsRunWhenOnlyNetworkDisplayNameChanges(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err := factory.run(0).establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	before := waitForState(t, ctx, session, Authenticated)
	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Campus Ethernet"))
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	if len(factory.creationInputs()) != 1 || got.SelectedNetworkBinding.DisplayName != "Campus Ethernet" {
		t.Fatalf("display-only update = %#v; factory calls = %d", got, len(factory.creationInputs()))
	}
	if got.State != Authenticated || got.AuthenticationEstablishedAt == nil || !got.AuthenticationEstablishedAt.Equal(*before.AuthenticationEstablishedAt) {
		t.Fatalf("display-only update cleared authentication: before=%#v after=%#v", before, got)
	}
}

func TestSessionPublishesReauthenticationBeforeChangedBindingCleanup(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First"))
	first := factory.run(0)
	defer first.unblock(nil)
	if err := first.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	authenticated := waitForState(t, ctx, session, Authenticated)
	if authenticated.AuthenticationEstablishedAt == nil {
		t.Fatal("first binding was not authenticated")
	}

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second"))
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	if got.SelectedNetworkBinding == nil || got.SelectedNetworkBinding.InterfaceID != "second" || got.State != Authenticating || got.StateReason != nil || got.AuthenticationEstablishedAt != nil {
		t.Fatalf("changed binding snapshot before cleanup = %#v", got)
	}
	assertNextRevision(t, "atomic authenticated binding reauthentication", got, authenticated)
	assertCleanupRequirement(t, first.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("changed binding overlapped runs: calls before cleanup = %d", calls)
	}

	first.unblock(blockingCommandFailure("canceled-old-binding"))
	second := waitForFactoryRun(t, ctx, factory, 1)
	if inputs := factory.creationInputs(); inputs[1].SelectedSystemNetworkBinding.NetworkInterface().InterfaceID != "second" {
		t.Fatalf("replacement run used wrong binding: %#v", inputs[1].SelectedSystemNetworkBinding)
	}
	second.unblock(nil)
}

func TestSessionWaitsForCanceledRunBeforeStartingReplacement(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First"))
	first := factory.run(0)
	if err := first.waitForStart(ctx); err != nil {
		t.Fatalf("first run did not start: %v", err)
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second"))
	if cause := first.waitForCancellation(ctx); cause == nil {
		t.Fatal("first run was not cancelled")
	}
	if got := len(factory.creationInputs()); got != 1 {
		t.Fatalf("factory creation count before first run finishes = %d, want 1", got)
	}
	first.unblock(nil)
	waitForFactoryRun(t, ctx, factory, 1).unblock(nil)
}

func TestSessionInitialBindingFactoryFailureDoesNotPublishAuthenticating(t *testing.T) {
	baseFactory := &controlledFactory{}
	factory := newBlockingCreationFailureFactory(baseFactory, 0)
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)
	defer factory.releaseFailure()

	initial := sessionSnapshot(t, ctx, session)
	drainRevisionEvents(session.RevisionEvents())
	applyResult := make(chan snapshotReply, 1)
	go func() {
		snapshot, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
		applyResult <- snapshotReply{snapshot: snapshot, err: err}
	}()
	factory.waitForFailureCreation(t, ctx)

	beforeFailure := session.currentSnapshot.Clone()
	if beforeFailure.State != WaitingForNetwork || beforeFailure.SelectedNetworkBinding == nil || beforeFailure.SelectedNetworkBinding.InterfaceID != "ethernet" || beforeFailure.AuthenticationEstablishedAt != nil {
		t.Fatalf("snapshot while Factory has not created a run = %#v", beforeFailure)
	}
	assertNextRevision(t, "initial usable binding", beforeFailure, initial)
	assertRevisionEvent(t, ctx, session.RevisionEvents(), beforeFailure)

	factory.releaseFailure()
	var blocked Snapshot
	select {
	case result := <-applyResult:
		if result.err != nil {
			t.Fatalf("applySystemNetworkSnapshot() error = %v", result.err)
		}
		blocked = result.snapshot
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if blocked.State != BlockedByError || blocked.StateReason == nil || blocked.StateReason.Code != StateReasonCodeProtocolRunCreationFailed || blocked.AuthenticationEstablishedAt != nil {
		t.Fatalf("snapshot after Factory failure = %#v", blocked)
	}
	assertNextRevision(t, "initial Factory failure", blocked, beforeFailure)
	assertRevisionEvent(t, ctx, session.RevisionEvents(), blocked)
}

func TestSessionRetryFactoryFailureDoesNotPublishAuthenticating(t *testing.T) {
	baseFactory := &controlledFactory{}
	factory := newBlockingCreationFailureFactory(baseFactory, 1)
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	scheduler := &manualRetryScheduler{}
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, Dependencies{
		Now:            func() time.Time { return time.Unix(100, 0) },
		RetryPolicy:    standardRetryPolicy(),
		RetryScheduler: scheduler,
	})
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)
	defer factory.releaseFailure()

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	baseFactory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{Code: "retry-failure", HandlingRecommendation: protocol.RetryAfterStandardDelay})
	waiting := waitForState(t, ctx, session, WaitingBeforeRetry)
	if waiting.NextRetryAt == nil {
		t.Fatal("retry did not publish a deadline")
	}
	drainRevisionEvents(session.RevisionEvents())

	scheduler.callback(0)()
	factory.waitForFailureCreation(t, ctx)
	beforeFailure := session.currentSnapshot.Clone()
	if beforeFailure.State != WaitingBeforeRetry || beforeFailure.NextRetryAt != nil || beforeFailure.AuthenticationEstablishedAt != nil {
		t.Fatalf("snapshot while retry Factory has not created a run = %#v", beforeFailure)
	}
	assertNextRevision(t, "retry deadline elapsed", beforeFailure, waiting)
	assertRevisionEvent(t, ctx, session.RevisionEvents(), beforeFailure)

	factory.releaseFailure()
	blocked := waitForState(t, ctx, session, BlockedByError)
	if blocked.StateReason == nil || blocked.StateReason.Code != StateReasonCodeProtocolRunCreationFailed || blocked.NextRetryAt != nil || blocked.AuthenticationEstablishedAt != nil {
		t.Fatalf("snapshot after retry Factory failure = %#v", blocked)
	}
	assertNextRevision(t, "retry Factory failure", blocked, beforeFailure)
	assertRevisionEvent(t, ctx, session.RevisionEvents(), blocked)
}

func TestSessionCancellationCauseWinsOverReturnedFailure(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First"))
	first := factory.run(0)
	if err := first.waitForStart(ctx); err != nil {
		t.Fatalf("first run did not start: %v", err)
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "second", "Second"))
	if cause := first.waitForCancellation(ctx); cause == nil {
		t.Fatal("first run was not cancelled")
	}
	first.unblock(&protocol.AuthenticationProtocolRunFailure{Code: "should-not-be-visible", Description: "returned after cancellation"})
	second := waitForFactoryRun(t, ctx, factory, 1)
	got := sessionSnapshot(t, ctx, session)
	if got.LastAuthenticationFailure != nil || got.State != Authenticating {
		t.Fatalf("cancellation lost precedence: %#v", got)
	}
	second.unblock(nil)
}

func TestSessionCurrentCancellationCauseWinsQueuedFailure(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	defer run.unblock(nil)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatal(err)
	}
	session.active.cancel(protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: protocol.TerminateWithoutLogout})
	session.post(authenticationProtocolRunFinishedEvent{generation: 1, failure: &protocol.AuthenticationProtocolRunFailure{Code: "queued-failure"}})
	got := sessionSnapshot(t, ctx, session)
	if got.LastAuthenticationFailure != nil || got.State == BlockedByError {
		t.Fatal("queued failure beat current cancellation cause")
	}
	defer waitForFactoryRun(t, ctx, factory, 1).unblock(nil)
}

func TestSessionRejectsCredentialMaterialFromPublicFailure(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	sentinel := definition.AuthenticationCredential.Password
	username := definition.AuthenticationCredential.Username
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{
		Code:        protocol.AuthenticationProtocolFailureCode("code-" + username + "-" + sentinel),
		Description: "description-" + username + "-" + sentinel,
	})
	got := waitForState(t, ctx, session, BlockedByError)
	if got.LastAuthenticationFailure == nil {
		t.Fatal("public failure was not retained")
	}
	if got.LastAuthenticationFailure.Code != "authentication_protocol_failure" || got.LastAuthenticationFailure.Description != "Authentication protocol failure." {
		t.Fatal("public failure did not use stable generic fallbacks")
	}
	if strings.Contains(string(got.LastAuthenticationFailure.Code), sentinel) || strings.Contains(string(got.LastAuthenticationFailure.Code), username) ||
		strings.Contains(got.LastAuthenticationFailure.Description, sentinel) ||
		strings.Contains(got.LastAuthenticationFailure.Description, username) ||
		strings.Contains(fmt.Sprint(got.LastAuthenticationFailure), sentinel) {
		t.Fatal("public failure contains credential material")
	}
}

func TestSessionUsesSafeFallbackForEmptyPublicFailureText(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{})
	got := waitForState(t, ctx, session, BlockedByError)
	if got.LastAuthenticationFailure == nil || got.LastAuthenticationFailure.Code != "authentication_protocol_failure" || got.LastAuthenticationFailure.Description != "Authentication protocol failure." {
		t.Fatal("empty public failure text did not receive safe fallbacks")
	}
}

func TestSessionNeverProjectsCredentialMaterialFromAnyFailureField(t *testing.T) {
	for _, field := range []string{"code", "description", "recommendation"} {
		t.Run(field, func(t *testing.T) {
			factory := &controlledFactory{}
			definition := validRuntimeDefinition(t)
			definition.AuthenticationProtocolFactory = factory
			session, _ := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(time.Now))
			session.Start()
			ctx := testContext(t)
			defer shutdownTestSession(t, session)
			failure := &protocol.AuthenticationProtocolRunFailure{Code: "safe-code", Description: "safe-description", HandlingRecommendation: protocol.RetryAfterStandardDelay}
			sentinel := definition.AuthenticationCredential.Password
			switch field {
			case "code":
				failure.Code = protocol.AuthenticationProtocolFailureCode(sentinel)
			case "description":
				failure.Description = sentinel
			case "recommendation":
				failure.HandlingRecommendation = protocol.AuthenticationProtocolFailureHandlingRecommendation(sentinel)
			}
			_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
			factory.run(0).unblock(failure)
			got := waitForState(t, ctx, session, BlockedByError).LastAuthenticationFailure
			if got == nil || strings.Contains(fmt.Sprint(got), sentinel) {
				t.Fatal("public failure contains credential material")
			}
		})
	}
}

func TestSessionDoesNotLeakRedactionOrFallbackCollisions(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	definition.AuthenticationCredential.Username = "[redacted]"
	definition.AuthenticationCredential.Password = "Authentication protocol failure."
	session, _ := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(time.Now))
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(&protocol.AuthenticationProtocolRunFailure{Code: "[redacted]", Description: "Authentication protocol failure.", HandlingRecommendation: protocol.RetryAfterStandardDelay})
	got := waitForState(t, ctx, session, BlockedByError).LastAuthenticationFailure
	if got == nil || strings.Contains(fmt.Sprint(got), definition.AuthenticationCredential.Username) || strings.Contains(fmt.Sprint(got), definition.AuthenticationCredential.Password) {
		t.Fatal("public projection contains collision secret")
	}
}

func TestSessionIgnoresStaleNetworkRevisions(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "current", "Current"))
	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "stale", "Stale"))
	if err != nil || got.SelectedNetworkBinding.InterfaceID != "current" || len(factory.creationInputs()) != 1 {
		t.Fatalf("stale network revision changed session: %#v; calls = %d; err = %v", got, len(factory.creationInputs()), err)
	}
}

func TestSessionSnapshotsAndRuntimeDefinitionAreCloned(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, SuspendAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	definition.Configuration.ProtocolContextOverride[0] = '!'
	_, _ = session.Activate(ctx)
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if got := string(factory.creationInputs()[0].ProtocolContextOverride); got != `{"network":"campus"}` {
		t.Fatalf("factory received aliased override %q", got)
	}
	first := sessionSnapshot(t, ctx, session)
	first.SelectedNetworkBinding.DisplayName = "mutated"
	second := sessionSnapshot(t, ctx, session)
	if second.SelectedNetworkBinding.DisplayName != "Ethernet" {
		t.Fatalf("snapshot is aliased: %#v", second)
	}
}

func TestSessionRevisionEventsCoalesce(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	select {
	case <-session.RevisionEvents():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Campus Ethernet"))
	final := sessionSnapshot(t, ctx, session)
	select {
	case event := <-session.RevisionEvents():
		if event.Revision != final.Revision || event.AuthenticationSessionID != final.AuthenticationSessionID {
			t.Fatalf("coalesced revision event = %#v, final snapshot = %#v", event, final)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestSessionSnapshotRevisionIsMonotonicAndNoOpsDoNotIncrement(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	initial := sessionSnapshot(t, ctx, session)
	activated, err := session.Activate(ctx)
	if err != nil {
		t.Fatalf("activate() error = %v", err)
	}
	assertSameRevision(t, "no-op activate without network", activated, initial)

	network := usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet")
	authenticating, err := session.ApplySystemNetworkSnapshot(ctx, network)
	if err != nil {
		t.Fatalf("applySystemNetworkSnapshot() error = %v", err)
	}
	run := factory.run(0)
	defer run.unblock(nil)
	assertRevisionIncreased(t, "initial binding and successful run", authenticating, initial)

	equivalent, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "ethernet", "Ethernet"))
	if err != nil {
		t.Fatalf("equivalent applySystemNetworkSnapshot() error = %v", err)
	}
	assertSameRevision(t, "equivalent higher network revision", equivalent, authenticating)

	stale, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "stale", "Stale"))
	if err != nil {
		t.Fatalf("stale applySystemNetworkSnapshot() error = %v", err)
	}
	assertSameRevision(t, "stale network revision", stale, equivalent)

	if err := run.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	authenticated := waitForState(t, ctx, session, Authenticated)
	assertNextRevision(t, "authentication establishment", authenticated, stale)
	session.post(authenticationEstablishedEvent{generation: 1})
	duplicate := sessionSnapshot(t, ctx, session)
	assertSameRevision(t, "duplicate establishment", duplicate, authenticated)

	displayUpdate, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 3, "ethernet", "Campus Ethernet"))
	if err != nil {
		t.Fatalf("display applySystemNetworkSnapshot() error = %v", err)
	}
	assertNextRevision(t, "public display-name update", displayUpdate, duplicate)
	activated, err = session.Activate(ctx)
	if err != nil {
		t.Fatalf("healthy activate() error = %v", err)
	}
	assertSameRevision(t, "healthy activate", activated, displayUpdate)

	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	if stopping.State != Stopping {
		t.Fatalf("suspend state = %q, want %q", stopping.State, Stopping)
	}
	assertNextRevision(t, "begin suspend", stopping, activated)
	repeatedSuspend, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("repeated suspend() error = %v", err)
	}
	assertSameRevision(t, "repeated suspend", repeatedSuspend, stopping)
	run.unblock(nil)
	suspended := waitForState(t, ctx, session, Suspended)
	assertNextRevision(t, "complete suspend", suspended, stopping)
}

func TestStoppingCannotBeOverwrittenByInputsOrCallbacks(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "first", "First"))
	run := factory.run(0)
	defer run.unblock(nil)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}
	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)

	if got, err := session.Activate(ctx); err != nil || got.State != Stopping || got.Intent != SuspendAuthentication {
		t.Fatalf("activate while stopping = (%#v, %v)", got, err)
	}
	if got, err := session.Restart(ctx); err != nil || got.State != Stopping || got.Intent != SuspendAuthentication {
		t.Fatalf("restart while stopping = (%#v, %v)", got, err)
	}
	if got, err := session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil)); err != nil || got.State != Stopping {
		t.Fatalf("network loss while stopping = (%#v, %v)", got, err)
	}
	session.post(authenticationEstablishedEvent{generation: 1})
	if got := sessionSnapshot(t, ctx, session); got.State != Stopping || got.Revision < stopping.Revision {
		t.Fatalf("callback overwrote stopping: %#v", got)
	}

	run.unblock(nil)
	_ = waitForState(t, ctx, session, Suspended)
}

func assertNextRevision(t *testing.T, operation string, got, previous Snapshot) {
	t.Helper()
	if got.Revision != previous.Revision+1 {
		t.Fatalf("%s revision = %d, want %d after revision %d", operation, got.Revision, previous.Revision+1, previous.Revision)
	}
}

func assertSameRevision(t *testing.T, operation string, got, previous Snapshot) {
	t.Helper()
	if got.Revision != previous.Revision {
		t.Fatalf("%s revision = %d, want unchanged %d", operation, got.Revision, previous.Revision)
	}
}

func assertRevisionIncreased(t *testing.T, operation string, got, previous Snapshot) {
	t.Helper()
	if got.Revision <= previous.Revision {
		t.Fatalf("%s revision = %d, want greater than %d", operation, got.Revision, previous.Revision)
	}
}

func assertRevisionEvent(t *testing.T, ctx context.Context, revisions <-chan RevisionEvent, snapshot Snapshot) {
	t.Helper()
	select {
	case event := <-revisions:
		if event.AuthenticationSessionID != snapshot.AuthenticationSessionID || event.Revision != snapshot.Revision {
			t.Fatalf("revision event = %#v, want session %q revision %d", event, snapshot.AuthenticationSessionID, snapshot.Revision)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func drainRevisionEvents(revisions <-chan RevisionEvent) {
	for {
		select {
		case <-revisions:
		default:
			return
		}
	}
}

func TestSessionBlocksWhenFactoryReturnsNilRun(t *testing.T) {
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = nilRunFactory{}
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err != nil || got.State != BlockedByError || got.StateReason == nil || got.StateReason.Code != StateReasonCodeProtocolRunCreationFailed {
		t.Fatalf("nil run snapshot = %#v; err = %v", got, err)
	}
	assertSafePublicCreationFailure(t, got)
}

func TestSessionBlocksSafelyWhenFactoryReturnsError(t *testing.T) {
	factory := &controlledFactory{creationError: errControlledFactoryCreation}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	got, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err != nil || got.State != BlockedByError || got.StateReason == nil || got.StateReason.Code != StateReasonCodeProtocolRunCreationFailed {
		t.Fatalf("factory error did not safely block the session: state=%q code=%q err=%v", got.State, stateReasonCode(got.StateReason), err)
	}
	assertSafePublicCreationFailure(t, got)
	if strings.Contains(fmt.Sprint(got.LastAuthenticationFailure), errControlledFactoryCreation.Error()) {
		t.Fatal("public creation failure leaked the factory diagnostic")
	}
}

// assertSafePublicCreationFailure proves a Factory error or nil Run produces a
// blocking public creation failure with safe static values, no retry and no
// leaked diagnostic in the public Snapshot.
func assertSafePublicCreationFailure(t *testing.T, got Snapshot) {
	t.Helper()
	if got.LastAuthenticationFailure == nil {
		t.Fatal("creation failure did not publish a public failure")
	}
	if got.LastAuthenticationFailure.Code != protocol.AuthenticationProtocolFailureCode(StateReasonCodeProtocolRunCreationFailed) {
		t.Fatalf("public failure code = %q, want %q", got.LastAuthenticationFailure.Code, StateReasonCodeProtocolRunCreationFailed)
	}
	if got.LastAuthenticationFailure.Description != "Unable to create authentication protocol run." {
		t.Fatalf("public failure description = %q, want static creation description", got.LastAuthenticationFailure.Description)
	}
	if got.LastAuthenticationFailure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
		t.Fatalf("public failure recommendation = %q, want block", got.LastAuthenticationFailure.HandlingRecommendation)
	}
	if got.NextRetryAt != nil {
		t.Fatal("creation failure scheduled a retry")
	}
	if got.AuthenticationEstablishedAt != nil {
		t.Fatal("creation failure retained an establishment time")
	}
}

func TestSessionIgnoresDuplicateAuthenticationEstablished(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err := factory.run(0).establish(ctx); err != nil {
		t.Fatalf("first establish() error = %v", err)
	}
	first := waitForState(t, ctx, session, Authenticated)
	session.post(authenticationEstablishedEvent{generation: 1})
	second := sessionSnapshot(t, ctx, session)
	if second.Revision != first.Revision || second.AuthenticationEstablishedAt == nil || !second.AuthenticationEstablishedAt.Equal(*first.AuthenticationEstablishedAt) {
		t.Fatal("duplicate establishment changed the public snapshot")
	}
}

func TestSessionStartIsIdempotent(t *testing.T) {
	factory := &controlledFactory{}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	session.Start()
	session.Start()
	select {
	case event := <-session.RevisionEvents():
		if event.Revision != 1 {
			t.Fatalf("initial revision = %d, want 1", event.Revision)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-session.RevisionEvents():
		t.Fatal("start() published more than one initial revision")
	default:
	}
}

func TestSessionNetworkLossCancelsWithTerminateWithoutLogout(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	defer run.unblock(nil)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}
	_, _ = session.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(2, time.Unix(2, 0), nil))
	cause := run.waitForCancellation(ctx)
	cancellation, ok := cause.(protocol.AuthenticationProtocolRunCancellationCause)
	if !ok || cancellation.CleanupRequirement != protocol.TerminateWithoutLogout {
		t.Fatal("network loss did not request terminate without logout")
	}
}

func TestSessionBlocksOnUncancelledNilExecuteResult(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(nil)
	got := waitForState(t, ctx, session, BlockedByError)
	if got.StateReason == nil || got.StateReason.Code != StateReasonCodeProtocolContractViolated {
		t.Fatalf("uncancelled nil result state reason = %q", stateReasonCode(got.StateReason))
	}
}

func TestSessionRecoversProtocolRunPanic(t *testing.T) {
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = panicRunFactory{}
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	got := waitForState(t, ctx, session, BlockedByError)
	if got.StateReason == nil || got.StateReason.Code != StateReasonCodeProtocolContractViolated || got.LastAuthenticationFailure == nil || got.LastAuthenticationFailure.Code != StateReasonCodeProtocolContractViolated {
		t.Fatalf("panic snapshot = %#v", got)
	}
}

func newTestSession(t *testing.T, factory *controlledFactory, intent Intent) *AuthenticationSession {
	t.Helper()
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	session, err := NewAuthenticationSession(definition, intent, testDependencies(func() time.Time { return time.Unix(100, 0) }))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	return session
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func shutdownTestSession(t *testing.T, session *AuthenticationSession) {
	t.Helper()
	ctx := testContext(t)
	if err := session.Shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

func sessionSnapshot(t *testing.T, ctx context.Context, session *AuthenticationSession) Snapshot {
	t.Helper()
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot() error = %v", err)
	}
	return snapshot
}

func waitForState(t *testing.T, ctx context.Context, session *AuthenticationSession, want State) Snapshot {
	t.Helper()
	for {
		got := sessionSnapshot(t, ctx, session)
		if got.State == want {
			return got
		}
		select {
		case <-ctx.Done():
			t.Fatalf("state = %q, want %q: %v", got.State, want, ctx.Err())
		default:
		}
	}
}

func waitForFactoryRun(t *testing.T, ctx context.Context, factory *controlledFactory, index int) *controlledRun {
	t.Helper()
	for {
		inputs := factory.creationInputs()
		if len(inputs) > index {
			return factory.run(index)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("factory did not create run %d: %v", index, ctx.Err())
		default:
		}
	}
}

func waitForInboxMessage(t *testing.T, ctx context.Context, session *AuthenticationSession) {
	t.Helper()
	for len(session.inbox) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
		}
	}
}

func usableSystemNetworkSnapshot(t *testing.T, revision uint64, interfaceID, displayName string) environment.Snapshot {
	t.Helper()
	networkInterface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:              environment.InterfaceID(interfaceID),
		DisplayName:              displayName,
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		HardwareAddress:          []byte{0, 1, 2, 3, 4, 5},
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24,
		}},
	})
	if err != nil {
		t.Fatalf("NewNetworkInterface() error = %v", err)
	}
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{networkInterface})
}

func assertExactFactoryInputs(t *testing.T, got protocol.AuthenticationProtocolRunCreationInputs, definition RuntimeDefinition, network environment.Snapshot) {
	t.Helper()
	if got.AuthenticationCredential.Username != definition.AuthenticationCredential.Username {
		t.Error("factory username differs")
	}
	if got.AuthenticationCredential.Password != definition.AuthenticationCredential.Password {
		t.Error("factory password differs")
	}
	if !bytes.Equal(got.InstitutionProtocolConfiguration, definition.InstitutionProfile.InstitutionProtocolConfiguration) {
		t.Error("factory institution configuration differs")
	}
	if !bytes.Equal(got.ProtocolContextOverride, definition.Configuration.ProtocolContextOverride) {
		t.Error("factory protocol context override differs")
	}
	if got.SystemHostInformation.HostName != definition.SystemHostInformation.HostName {
		t.Error("factory host name differs")
	}
	if got.SystemHostInformation.OperatingSystemFamily != definition.SystemHostInformation.OperatingSystemFamily {
		t.Error("factory operating system family differs")
	}
	if got.SystemHostInformation.OperatingSystemRelease != definition.SystemHostInformation.OperatingSystemRelease {
		t.Error("factory operating system release differs")
	}
	if got.SystemHostInformation.MachineArchitecture != definition.SystemHostInformation.MachineArchitecture {
		t.Error("factory machine architecture differs")
	}
	interfaces := network.Interfaces()
	if len(interfaces) != 1 {
		t.Fatal("test network does not have one interface")
	}
	wantInterface := interfaces[0]
	gotInterface := got.SelectedSystemNetworkBinding.NetworkInterface()
	if gotInterface.InterfaceID != wantInterface.InterfaceID {
		t.Error("factory binding interface ID differs")
	}
	if gotInterface.DisplayName != wantInterface.DisplayName {
		t.Error("factory binding display name differs")
	}
	if gotInterface.OperationalState != wantInterface.OperationalState {
		t.Error("factory binding operational state differs")
	}
	if gotInterface.PhysicalMedium != wantInterface.PhysicalMedium {
		t.Error("factory binding physical medium differs")
	}
	if gotInterface.AddressAssignmentMethod != wantInterface.AddressAssignmentMethod {
		t.Error("factory binding address assignment method differs")
	}
	if !bytes.Equal(gotInterface.HardwareAddress(), wantInterface.HardwareAddress()) {
		t.Error("factory binding hardware address differs")
	}
	if !equalIPv4Assignments(gotInterface.IPv4AddressAssignments(), wantInterface.IPv4AddressAssignments()) {
		t.Error("factory binding IPv4 assignments differ")
	}
	if !equalAddresses(gotInterface.DefaultIPv4GatewayAddresses(), wantInterface.DefaultIPv4GatewayAddresses()) {
		t.Error("factory binding default gateways differ")
	}
	if !equalAddresses(gotInterface.DNSServerAddresses(), wantInterface.DNSServerAddresses()) {
		t.Error("factory binding DNS servers differ")
	}
	gotDHCP, gotHasDHCP := gotInterface.DHCPServerIPv4Address()
	wantDHCP, wantHasDHCP := wantInterface.DHCPServerIPv4Address()
	if gotHasDHCP != wantHasDHCP || (gotHasDHCP && gotDHCP != wantDHCP) {
		t.Error("factory binding DHCP server differs")
	}
	wantAssignment := wantInterface.IPv4AddressAssignments()[0]
	if got.SelectedSystemNetworkBinding.LocalIPv4AddressAssignment() != wantAssignment {
		t.Error("factory binding selected IPv4 assignment differs")
	}
}

func equalIPv4Assignments(left, right []environment.IPv4AddressAssignment) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalAddresses(left, right []netip.Addr) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func stateReasonCode(reason *StateReason) string {
	if reason == nil {
		return ""
	}
	return reason.Code
}

var _ protocol.AuthenticationProtocolRun = (*controlledRun)(nil)

type nilRunFactory struct{}

func (nilRunFactory) ProtocolID() protocol.AuthenticationProtocolID { return "test-protocol" }
func (nilRunFactory) ValidateInstitutionProtocolConfiguration(protocol.InstitutionProtocolConfiguration) error {
	return nil
}
func (nilRunFactory) ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride) error {
	return nil
}
func (nilRunFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return nil, nil
}

type panicRunFactory struct{ nilRunFactory }

func (panicRunFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return panicRun{}, nil
}

type panicRun struct{}

func (panicRun) Execute(context.Context, protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	panic("test panic")
}

type blockingCreationFactory struct {
	*controlledFactory
	creationEntered chan struct{}
	releaseCreation chan struct{}
	firstCreation   chan struct{}
}

type blockingCreationFailureFactory struct {
	*controlledFactory
	mu      sync.Mutex
	calls   int
	failAt  int
	entered chan struct{}
	release chan struct{}
}

func newBlockingCreationFailureFactory(factory *controlledFactory, failAt int) *blockingCreationFailureFactory {
	return &blockingCreationFailureFactory{
		controlledFactory: factory,
		failAt:            failAt,
		entered:           make(chan struct{}, 1),
		release:           make(chan struct{}, 1),
	}
}

func (factory *blockingCreationFailureFactory) CreateAuthenticationProtocolRun(inputs protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	factory.mu.Lock()
	call := factory.calls
	factory.calls++
	factory.mu.Unlock()
	if call != factory.failAt {
		return factory.controlledFactory.CreateAuthenticationProtocolRun(inputs)
	}
	factory.entered <- struct{}{}
	<-factory.release
	return nil, errControlledFactoryCreation
}

func (factory *blockingCreationFailureFactory) waitForFailureCreation(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-factory.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func (factory *blockingCreationFailureFactory) releaseFailure() {
	select {
	case factory.release <- struct{}{}:
	default:
	}
}

func newBlockingCreationFactory(factory *controlledFactory) *blockingCreationFactory {
	return &blockingCreationFactory{
		controlledFactory: factory,
		creationEntered:   make(chan struct{}, 1),
		releaseCreation:   make(chan struct{}),
		firstCreation:     make(chan struct{}, 1),
	}
}

func (factory *blockingCreationFactory) CreateAuthenticationProtocolRun(inputs protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	select {
	case factory.firstCreation <- struct{}{}:
		factory.creationEntered <- struct{}{}
		select {
		case <-factory.releaseCreation:
		case <-time.After(time.Second):
			return nil, errControlledFactoryCreation
		}
	default:
	}
	return factory.controlledFactory.CreateAuthenticationProtocolRun(inputs)
}

// captureDiagnostics records session diagnostic events for assertion. It never
// alters Session behavior.
type captureDiagnostics struct {
	mu             sync.Mutex
	snapshots      []Snapshot
	commands       []string
	generations    []uint64
	retryScheduled []time.Time
}

func (d *captureDiagnostics) SessionSnapshot(snapshot Snapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshots = append(d.snapshots, snapshot)
}

func (d *captureDiagnostics) SessionCommand(command string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands = append(d.commands, command)
}

func (d *captureDiagnostics) ProtocolRunGeneration(generation uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.generations = append(d.generations, generation)
}

func (d *captureDiagnostics) RetryScheduled(nextRetryAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.retryScheduled = append(d.retryScheduled, nextRetryAt)
}

func (d *captureDiagnostics) snapshotCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.snapshots)
}

func newDiagnosticsSession(t *testing.T, factory *controlledFactory, capture Diagnostics) *AuthenticationSession {
	t.Helper()
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	deps := Dependencies{
		Now:            func() time.Time { return time.Unix(100, 0) },
		RetryPolicy:    unavailableRetryPolicy{},
		RetryScheduler: noOpRetryScheduler{},
		Diagnostics:    capture,
	}
	authSession, err := NewAuthenticationSession(definition, MaintainAuthentication, deps)
	if err != nil {
		t.Fatalf("NewAuthenticationSession: %v", err)
	}
	authSession.Start()
	return authSession
}

// TestSessionDiagnosticsObservesEveryCommittedRevision proves SessionSnapshot is
// invoked once per committed revision without coalescing, so no intermediate
// public revision is lost.
func TestSessionDiagnosticsObservesEveryCommittedRevision(t *testing.T) {
	factory := &controlledFactory{}
	capture := &captureDiagnostics{}
	authSession := newDiagnosticsSession(t, factory, capture)
	defer func() { _ = authSession.Shutdown(context.Background()) }()

	ctx := testContext(t)
	// Wait for the run goroutine to start and publish the initial revision.
	if _, err := authSession.Snapshot(ctx); err != nil {
		t.Fatalf("initial Snapshot: %v", err)
	}
	initialCount := capture.snapshotCount()
	if initialCount < 1 {
		t.Fatalf("expected initial revision observed, got %d", initialCount)
	}
	_, _ = authSession.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	_ = waitForState(t, ctx, authSession, Authenticating)

	if capture.snapshotCount() < 2 {
		t.Fatalf("expected at least 2 observed snapshots (initial + network), got %d", capture.snapshotCount())
	}
}

// TestSessionDiagnosticsRecordsCommandAndGeneration proves SessionCommand and
// ProtocolRunGeneration are recorded without changing Session behavior.
func TestSessionDiagnosticsRecordsCommandAndGeneration(t *testing.T) {
	factory := &controlledFactory{}
	capture := &captureDiagnostics{}
	authSession := newDiagnosticsSession(t, factory, capture)
	defer func() { _ = authSession.Shutdown(context.Background()) }()

	ctx := testContext(t)
	_, _ = authSession.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	_ = waitForFactoryRun(t, ctx, factory, 0)

	capture.mu.Lock()
	commands := append([]string(nil), capture.commands...)
	generations := append([]uint64(nil), capture.generations...)
	capture.mu.Unlock()

	if len(generations) == 0 {
		t.Fatal("expected ProtocolRunGeneration to be called after run creation")
	}
	found := false
	for _, c := range commands {
		if c == "apply_network_snapshot" {
			found = true
		}
	}
	if !found {
		t.Errorf("apply_network_snapshot command not recorded; commands=%v", commands)
	}
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

func TestSessionAutoReconnectFalseSuspendReachesSuspended(t *testing.T) {
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

func TestExplicitSessionUnavailableNeverRunsAndLossWaits(t *testing.T) {
	for _, reconnect := range []bool{true, false} {
		t.Run(fmt.Sprint(reconnect), func(t *testing.T) {
			ctx := testContext(t)
			factory := &controlledFactory{holdCancellation: true}
			definition := validRuntimeDefinition(t)
			definition.AuthenticationProtocolFactory = factory
			definition.AutoReconnect = reconnect
			definition.Configuration.NetworkBindingPolicy = NetworkBindingPolicy{Mode: ExplicitInterfaceAndLocalIPv4, InterfaceID: "target", LocalIPv4Address: netip.MustParseAddr("192.0.2.10")}
			s, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(func() time.Time { return time.Unix(100, 0) }))
			if err != nil {
				t.Fatal(err)
			}
			s.Start()
			defer shutdownTestSession(t, s)
			check := func(got Snapshot) {
				t.Helper()
				if got.State != WaitingForNetwork || got.SelectedNetworkBinding != nil || got.StateReason == nil || got.StateReason.Code != StateReasonCodeNetworkBindingUnavailable || got.StateReason.Description != "The selected network binding is unavailable." {
					t.Fatal("unavailable binding state differs")
				}
			}
			check(sessionSnapshot(t, ctx, s))
			got, err := s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "other", "Other"))
			if err != nil {
				t.Fatal(err)
			}
			check(got)
			if len(factory.creationInputs()) != 0 {
				t.Fatal("unavailable target started Run")
			}
			_, err = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "target", "Target"))
			if err != nil {
				t.Fatal(err)
			}
			run := waitForFactoryRun(t, ctx, factory, 0)
			defer run.unblock(nil)
			if err = run.establish(ctx); err != nil {
				t.Fatal(err)
			}
			waitForState(t, ctx, s, Authenticated)
			got, err = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 3, "other", "Other"))
			if err != nil {
				t.Fatal(err)
			}
			check(got)
			assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithoutLogout)
			_, err = s.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 4, "target", "Target"))
			if err != nil {
				t.Fatal(err)
			}
			if len(factory.creationInputs()) != 1 {
				t.Fatal("replacement Run overlapped cleanup")
			}
			run.unblock(nil)
			if reconnect {
				next := waitForFactoryRun(t, ctx, factory, 1)
				next.unblock(nil)
			} else {
				got = waitForState(t, ctx, s, BlockedByError)
				if got.StateReason == nil || got.StateReason.Code != StateReasonCodeAutomaticReconnectDisabled {
					t.Fatal("automatic reconnect policy lost")
				}
			}
		})
	}
}
