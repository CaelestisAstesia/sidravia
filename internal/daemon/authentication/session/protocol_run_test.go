package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	profilevalue "sidravia/internal/connection/profile"
	"sidravia/internal/daemon/authentication/protocol"
)

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
		Now:            scheduler.bindClock(func() time.Time { return time.Unix(100, 0) }),
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

	_, _ = session.ApplySystemNetworkSnapshot(ctx, withStandby(t, usableSystemNetworkSnapshot(t, 1, "first", "First"), "second"))
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

func stateReasonCode(reason *StateReason) string {
	if reason == nil {
		return ""
	}
	return reason.Code
}

type nilRunFactory struct{}

func (nilRunFactory) ProtocolID() profilevalue.ProtocolID { return "test-protocol" }
func (nilRunFactory) ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error {
	return nil
}
func (nilRunFactory) ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error {
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

func (nilRunFactory) ValidateUsername(string) error { return nil }
