package session

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func TestCompleteShutdownClosesDoneBeforeReleasingWaiters(t *testing.T) {
	firstReply := make(chan error, 1)
	blockedReply := make(chan error)
	session := &AuthenticationSession{
		done:            make(chan struct{}),
		shutdownReplies: []chan error{firstReply, blockedReply},
	}
	completed := make(chan struct{})
	go func() {
		session.completeShutdown()
		close(completed)
	}()

	ctx := testContext(t)
	for len(firstReply) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("completeShutdown() did not begin releasing waiters")
		default:
			runtime.Gosched()
		}
	}
	select {
	case <-session.done:
	default:
		<-blockedReply
		<-completed
		t.Fatal("completeShutdown() released a waiter before closing done")
	}

	if err := <-firstReply; err != nil {
		t.Fatalf("first shutdown reply = %v, want nil", err)
	}
	if err := <-blockedReply; err != nil {
		t.Fatalf("blocked shutdown reply = %v, want nil", err)
	}
	<-completed
}

func TestActivateDoesNotRestartHealthyAuthenticatedRun(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	if err := run.establish(ctx); err != nil {
		t.Fatalf("establish() error = %v", err)
	}
	_ = waitForState(t, ctx, session, Authenticated)

	got, err := session.Activate(ctx)
	if err != nil {
		t.Fatalf("activate() error = %v", err)
	}
	if got.State != Authenticated || got.AuthenticationEstablishedAt == nil {
		t.Fatalf("activate() snapshot = %#v, want healthy authenticated state", got)
	}
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("activate() created %d protocol runs, want 1", calls)
	}
	if cause := context.Cause(session.active.context); cause != nil {
		t.Fatalf("activate() canceled healthy run: %v", cause)
	}
}

func TestActivateDoesNotBypassWaitingBeforeRetry(t *testing.T) {
	policy := standardRetryPolicy()
	scheduler := &manualRetryScheduler{}
	factory := &controlledFactory{}
	session := newRetryTestSession(t, factory, policy, scheduler, func() time.Time { return time.Unix(100, 0) })
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	scheduleStandardRetry(t, ctx, session, factory)
	before := sessionSnapshot(t, ctx, session)
	cancellation := scheduler.cancellation(0)

	got, err := session.Activate(ctx)
	if err != nil {
		t.Fatalf("activate() error = %v", err)
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("activate() changed waiting retry snapshot:\n got  %#v\n want %#v", got, before)
	}
	if cancellation.cancelled() || cancellation.cancelCount() != 0 {
		t.Fatalf("activate() canceled retry: cancelled=%v count=%d", cancellation.cancelled(), cancellation.cancelCount())
	}
	if scheduler.count() != 1 {
		t.Fatalf("activate() rescheduled retry: schedules=%d, want 1", scheduler.count())
	}
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("activate() created %d runs while waiting, want 1", calls)
	}

	scheduler.callback(0)()
	second := waitForFactoryRun(t, ctx, factory, 1)
	second.unblock(nil)
}

func TestActivateDoesNotBypassBlockedByError(t *testing.T) {
	factory := &controlledFactory{}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(blockingCommandFailure("blocked-failure"))
	before := waitForState(t, ctx, session, BlockedByError)

	got, err := session.Activate(ctx)
	if err != nil {
		t.Fatalf("activate() error = %v", err)
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("activate() changed blocked snapshot:\n got  %#v\n want %#v", got, before)
	}
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("activate() created %d runs while blocked, want 1", calls)
	}

	if _, err := session.Restart(ctx); err != nil {
		t.Fatalf("restart() error = %v", err)
	}
	second := waitForFactoryRun(t, ctx, factory, 1)
	second.unblock(nil)
}

func TestSuspendCancelsRunWithBestEffortLogout(t *testing.T) {
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

	got, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	if got.Intent != SuspendAuthentication || got.State != Stopping || got.StateReason != nil || got.AuthenticationEstablishedAt != nil || got.NextRetryAt != nil {
		t.Fatalf("suspend() snapshot = %#v", got)
	}
	assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	repeated, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("repeated suspend() error = %v", err)
	}
	if repeated.State != Stopping || repeated.Revision != got.Revision {
		t.Fatalf("repeated suspend() snapshot = %#v, want unchanged stopping revision %d", repeated, got.Revision)
	}
	select {
	case cause := <-run.canceled:
		t.Fatalf("repeated suspend requested a second cleanup: %v", cause)
	default:
	}
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("suspend() created %d protocol runs, want 1", calls)
	}
	cleanupFailure := &protocol.AuthenticationProtocolRunFailure{
		Code:        "logout_cleanup_failed",
		Description: "Best-effort logout cleanup failed.",
	}
	run.unblock(cleanupFailure)
	suspended := waitForState(t, ctx, session, Suspended)
	if suspended.Revision != got.Revision+1 || suspended.LastAuthenticationFailure != nil {
		t.Fatalf("completed suspension snapshot = %#v", suspended)
	}
	if session.lastStopCleanupFailure != cleanupFailure {
		t.Fatalf("private cleanup failure = %#v, want original internal failure", session.lastStopCleanupFailure)
	}
}

func TestSuspendWithoutActiveRunStillPublishesStoppingBeforeSuspended(t *testing.T) {
	session := newTestSession(t, &controlledFactory{}, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	initial := sessionSnapshot(t, ctx, session)
	if initial.State != WaitingForNetwork {
		t.Fatalf("initial state = %q, want %q", initial.State, WaitingForNetwork)
	}
	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	if stopping.State != Stopping || stopping.Intent != SuspendAuthentication {
		t.Fatalf("suspend() snapshot = %#v, want stopping", stopping)
	}
	suspended := waitForState(t, ctx, session, Suspended)
	if suspended.Revision != stopping.Revision+1 {
		t.Fatalf("suspended revision = %d, want %d", suspended.Revision, stopping.Revision+1)
	}
}

func TestSuspendBlockedSessionCompletesWithoutCreatingRun(t *testing.T) {
	factory := &controlledFactory{creationError: errControlledFactoryCreation}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	blocked, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	if err != nil || blocked.State != BlockedByError {
		t.Fatalf("blocked snapshot = %#v; error = %v", blocked, err)
	}
	stopping, err := session.Suspend(ctx)
	if err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	if stopping.State != Stopping {
		t.Fatalf("suspend() state = %q, want %q", stopping.State, Stopping)
	}
	_ = waitForState(t, ctx, session, Suspended)
	if calls := len(factory.creationInputs()); calls != 1 {
		t.Fatalf("stop created another protocol run: calls = %d", calls)
	}
}

func TestRestartClearsBlockAndStartsNewGeneration(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(blockingCommandFailure("previous-failure"))
	blocked := waitForState(t, ctx, session, BlockedByError)
	if blocked.LastAuthenticationFailure == nil {
		t.Fatal("blocking failure was not recorded")
	}
	wantFailure := *blocked.LastAuthenticationFailure
	if _, err := session.Suspend(ctx); err != nil {
		t.Fatalf("suspend() error = %v", err)
	}

	restarting, err := session.Restart(ctx)
	if err != nil {
		t.Fatalf("restart() error = %v", err)
	}
	if restarting.Intent != MaintainAuthentication || restarting.State != Authenticating || restarting.StateReason != nil || restarting.AuthenticationEstablishedAt != nil || restarting.NextRetryAt != nil {
		t.Fatalf("restart() snapshot = %#v", restarting)
	}
	if restarting.LastAuthenticationFailure == nil || *restarting.LastAuthenticationFailure != wantFailure {
		t.Fatalf("restart() failure = %#v, want retained %#v", restarting.LastAuthenticationFailure, wantFailure)
	}

	second := waitForFactoryRun(t, ctx, factory, 1)
	defer second.unblock(nil)
	if err := second.establish(ctx); err != nil {
		t.Fatalf("second establish() error = %v", err)
	}
	authenticated := waitForState(t, ctx, session, Authenticated)
	if authenticated.LastAuthenticationFailure != nil || authenticated.AuthenticationEstablishedAt == nil {
		t.Fatalf("new establishment did not clear prior failure: %#v", authenticated)
	}

	restarting, err = session.Restart(ctx)
	if err != nil {
		t.Fatalf("second restart() error = %v", err)
	}
	if restarting.State != Authenticating || restarting.AuthenticationEstablishedAt != nil {
		t.Fatalf("second restart() snapshot = %#v", restarting)
	}
	assertCleanupRequirement(t, second.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	if calls := len(factory.creationInputs()); calls != 2 {
		t.Fatalf("restart overlapped generations: factory calls = %d, want 2 before cleanup", calls)
	}
	second.unblock(blockingCommandFailure("canceled-failure"))
	third := waitForFactoryRun(t, ctx, factory, 2)
	third.unblock(nil)
}

func TestReplaceRuntimeDefinitionIsAtomicAndUsesBestEffortLogout(t *testing.T) {
	assertReplacementValidationPrecedesEnqueue(t)

	oldFactory := &controlledFactory{holdCancellation: true}
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = oldFactory
	session, err := NewAuthenticationSession(definition, MaintainAuthentication, testDependencies(nil))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	ctx := testContext(t)
	defer shutdownTestSession(t, session)

	network := usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet")
	_, _ = session.ApplySystemNetworkSnapshot(ctx, network)
	oldRun := oldFactory.run(0)
	defer oldRun.unblock(nil)
	if err := oldRun.waitForStart(ctx); err != nil {
		t.Fatalf("old run did not start: %v", err)
	}

	newFactory := &controlledFactory{}
	replacement := replacementRuntimeDefinition(t, newFactory)
	got, err := session.ReplaceRuntimeDefinition(ctx, replacement)
	if err != nil {
		t.Fatalf("replaceRuntimeDefinition() error = %v", err)
	}
	if got.AuthenticationSessionID != replacement.Configuration.AuthenticationSessionID ||
		got.DisplayName != replacement.Configuration.DisplayName ||
		got.InstitutionProfileID != replacement.InstitutionProfile.InstitutionProfileID ||
		got.InstitutionDisplayName != replacement.InstitutionProfile.DisplayName ||
		got.AccountLabel != replacement.AccountLabel() {
		t.Fatalf("replacement snapshot mixed runtime definitions: %#v", got)
	}
	if got.State != Authenticating || got.StateReason != nil || got.AuthenticationEstablishedAt != nil || got.NextRetryAt != nil {
		t.Fatalf("replacement did not reset runtime state: %#v", got)
	}
	assertCleanupRequirement(t, oldRun.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	if calls := len(newFactory.creationInputs()); calls != 0 {
		t.Fatalf("replacement overlapped old run with %d new runs", calls)
	}

	oldRun.unblock(blockingCommandFailure("canceled-old-definition-failure"))
	newRun := waitForFactoryRun(t, ctx, newFactory, 0)
	assertExactFactoryInputs(t, newFactory.creationInputs()[0], replacement, network)
	newRun.unblock(nil)
}

func TestShutdownWaitsForRunCleanupAndRejectsNewMessages(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	defer run.unblock(nil)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}

	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- session.Shutdown(ctx) }()
	assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	select {
	case err := <-shutdownResult:
		t.Fatalf("shutdown returned before Execute cleanup: %v", err)
	default:
	}
	select {
	case <-session.done:
		t.Fatal("done closed before Execute cleanup")
	default:
	}
	if _, err := session.Snapshot(ctx); !errors.Is(err, ErrAuthenticationSessionClosed) {
		t.Fatalf("snapshot during shutdown error = %v, want %v", err, ErrAuthenticationSessionClosed)
	}

	run.unblock(blockingCommandFailure("failure-after-shutdown-cancel"))
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatalf("shutdown() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	inboxLength := len(session.inbox)
	closedCalls := []struct {
		name string
		call func() error
	}{
		{name: "snapshot", call: func() error { _, err := session.Snapshot(ctx); return err }},
		{name: "network snapshot", call: func() error {
			_, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "wifi", "Wi-Fi"))
			return err
		}},
		{name: "activate", call: func() error { _, err := session.Activate(ctx); return err }},
		{name: "suspend", call: func() error { _, err := session.Suspend(ctx); return err }},
		{name: "restart", call: func() error { _, err := session.Restart(ctx); return err }},
		{name: "replace", call: func() error {
			invalid := replacementRuntimeDefinition(t, &controlledFactory{})
			invalid.Configuration.AuthenticationSessionID = ""
			_, err := session.ReplaceRuntimeDefinition(ctx, invalid)
			return err
		}},
		{name: "shutdown", call: func() error { return session.Shutdown(ctx) }},
	}
	for _, test := range closedCalls {
		if err := test.call(); !errors.Is(err, ErrAuthenticationSessionClosed) {
			t.Errorf("%s after shutdown error = %v, want %v", test.name, err, ErrAuthenticationSessionClosed)
		}
	}
	if got := len(session.inbox); got != inboxLength {
		t.Fatalf("closed calls changed inbox length from %d to %d", inboxLength, got)
	}
	assertRevisionEventsOpen(t, session.RevisionEvents())
}

func TestShutdownClosesAdmissionBeforeItsCommandCanRun(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}

	blockedReply := make(chan snapshotReply)
	select {
	case session.inbox <- snapshotQuery{reply: blockedReply}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitForInboxLength(t, ctx, session, 0)

	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- session.Shutdown(ctx) }()
	loopReleased := false
	defer func() {
		select {
		case <-session.done:
			return
		default:
		}
		if !loopReleased {
			go func() { <-blockedReply }()
		}
		run.unblock(nil)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		select {
		case <-session.done:
		case <-cleanupCtx.Done():
			t.Errorf("session cleanup after test: %v", cleanupCtx.Err())
		}
	}()

	waitForAdmissionClosed(t, ctx, session)
	waitForInboxLength(t, ctx, session, 1)
	inboxLength := len(session.inbox)
	callCtx, cancelCalls := context.WithTimeout(context.Background(), time.Second)
	defer cancelCalls()
	for _, test := range closedSessionCalls(t, callCtx, session) {
		if err := test.call(); !errors.Is(err, ErrAuthenticationSessionClosed) {
			t.Errorf("%s after shutdown admission closed error = %v, want %v", test.name, err, ErrAuthenticationSessionClosed)
		}
	}
	if got := len(session.inbox); got != inboxLength {
		t.Fatalf("rejected calls changed inbox length from %d to %d", inboxLength, got)
	}
	select {
	case err := <-shutdownResult:
		t.Fatalf("shutdown returned before its command ran and Execute cleaned up: %v", err)
	default:
	}

	select {
	case <-blockedReply:
		loopReleased = true
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	select {
	case err := <-shutdownResult:
		t.Fatalf("shutdown returned before Execute cleanup: %v", err)
	default:
	}
	run.unblock(nil)
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatalf("shutdown() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClosedSessionErrorPrecedesCanceledContextAndValidation(t *testing.T) {
	session := newTestSession(t, &controlledFactory{}, SuspendAuthentication)
	ctx := testContext(t)
	if err := session.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range closedSessionCalls(t, canceledCtx, session) {
		if err := test.call(); !errors.Is(err, ErrAuthenticationSessionClosed) {
			t.Errorf("%s error = %v, want closed error before canceled context or validation", test.name, err)
		}
	}
}

func TestCanceledShutdownEnqueueReopensAdmission(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	run := factory.run(0)
	if err := run.waitForStart(ctx); err != nil {
		t.Fatalf("run did not start: %v", err)
	}

	blockedReply := make(chan snapshotReply)
	select {
	case session.inbox <- snapshotQuery{reply: blockedReply}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitForInboxLength(t, ctx, session, 0)
	for index := 0; index < cap(session.inbox); index++ {
		session.inbox <- snapshotQuery{reply: make(chan snapshotReply, 1)}
	}

	loopReleased := false
	defer func() {
		select {
		case <-session.done:
			return
		default:
		}
		if !loopReleased {
			go func() { <-blockedReply }()
		}
		run.unblock(nil)
		if session.closed.Load() {
			session.closed.Store(false)
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = session.Shutdown(cleanupCtx)
	}()

	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- session.Shutdown(shutdownCtx) }()
	waitForAdmissionClosed(t, ctx, session)
	cancelShutdown()
	select {
	case err := <-shutdownResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled shutdown enqueue error = %v, want %v", err, context.Canceled)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitForAdmissionOpen(t, ctx, session)

	select {
	case <-blockedReply:
		loopReleased = true
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitForInboxLength(t, ctx, session, 0)
	if _, err := session.Snapshot(ctx); err != nil {
		t.Fatalf("snapshot after canceled shutdown enqueue error = %v", err)
	}

	finalShutdown := make(chan error, 1)
	go func() { finalShutdown <- session.Shutdown(ctx) }()
	assertCleanupRequirement(t, run.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	run.unblock(nil)
	select {
	case err := <-finalShutdown:
		if err != nil {
			t.Fatalf("final shutdown error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCanceledRunDoesNotOverwriteLastAuthenticationFailure(t *testing.T) {
	factory := &controlledFactory{holdCancellation: true}
	session := newTestSession(t, factory, MaintainAuthentication)
	ctx := testContext(t)
	defer func() {
		if !session.closed.Load() {
			shutdownTestSession(t, session)
		}
	}()

	_, _ = session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 1, "ethernet", "Ethernet"))
	factory.run(0).unblock(blockingCommandFailure("retained-failure"))
	blocked := waitForState(t, ctx, session, BlockedByError)
	if blocked.LastAuthenticationFailure == nil {
		t.Fatal("blocking failure was not recorded")
	}
	want := *blocked.LastAuthenticationFailure

	if _, err := session.Restart(ctx); err != nil {
		t.Fatalf("restart() error = %v", err)
	}
	second := waitForFactoryRun(t, ctx, factory, 1)
	defer second.unblock(nil)
	if _, err := session.Suspend(ctx); err != nil {
		t.Fatalf("suspend() error = %v", err)
	}
	assertCleanupRequirement(t, second.waitForCancellation(ctx), protocol.TerminateWithBestEffortLogout)
	second.unblock(blockingCommandFailure("must-not-overwrite"))
	got := waitForState(t, ctx, session, Suspended)
	if got.LastAuthenticationFailure == nil || *got.LastAuthenticationFailure != want {
		t.Fatalf("canceled run changed retained failure: %#v", got)
	}
	if err := session.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}

func assertReplacementValidationPrecedesEnqueue(t *testing.T) {
	t.Helper()
	session, err := NewAuthenticationSession(validRuntimeDefinition(t), MaintainAuthentication, testDependencies(nil))
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	invalid := validRuntimeDefinition(t)
	invalid.Configuration.AuthenticationSessionID = ""

	callContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := session.ReplaceRuntimeDefinition(callContext, invalid)
		result <- err
	}()
	deadline := testContext(t)
	for {
		select {
		case err := <-result:
			if err == nil || err.Error() != "authentication session ID is required" {
				t.Fatalf("invalid replacement error = %v", err)
			}
			if messages := len(session.inbox); messages != 0 {
				t.Fatalf("invalid replacement enqueued %d messages", messages)
			}
			return
		case <-deadline.Done():
			cancel()
			t.Fatal(deadline.Err())
		default:
			if messages := len(session.inbox); messages != 0 {
				cancel()
				<-result
				t.Fatalf("invalid replacement enqueued %d messages", messages)
			}
		}
	}
}

func replacementRuntimeDefinition(t *testing.T, factory *controlledFactory) RuntimeDefinition {
	t.Helper()
	replacement := validRuntimeDefinition(t)
	replacement.Configuration.AuthenticationSessionID = "session-2"
	replacement.Configuration.DisplayName = "Replacement network"
	replacement.Configuration.InstitutionProfileID = "profile-2"
	replacement.Configuration.CredentialID = "credential-2"
	replacement.Configuration.ProtocolContextOverride = []byte(`{"network":"replacement"}`)
	replacement.AuthenticationCredential.Username = "replacement-account"
	replacement.AuthenticationCredential.Password = "replacement-secret"
	replacement.InstitutionProfile.InstitutionProfileID = "profile-2"
	replacement.InstitutionProfile.DisplayName = "Replacement institution"
	replacement.InstitutionProfile.InstitutionProtocolConfiguration = []byte(`{"realm":"replacement"}`)
	replacement.AuthenticationProtocolFactory = factory
	replacement.SystemHostInformation.HostName = "replacement-host"
	replacement.SystemHostInformation.OperatingSystemFamily = "replacement-os"
	replacement.SystemHostInformation.OperatingSystemRelease = "replacement-release"
	replacement.SystemHostInformation.MachineArchitecture = "replacement-architecture"
	return replacement
}

func blockingCommandFailure(code protocol.AuthenticationProtocolFailureCode) *protocol.AuthenticationProtocolRunFailure {
	return &protocol.AuthenticationProtocolRunFailure{
		Code:                   code,
		Description:            string(code),
		HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange,
	}
}

func assertCleanupRequirement(t *testing.T, cause error, want protocol.AuthenticationProtocolRunCleanupRequirement) {
	t.Helper()
	cancellation, ok := cause.(protocol.AuthenticationProtocolRunCancellationCause)
	if !ok {
		t.Fatalf("cancellation cause type = %T, want protocol.AuthenticationProtocolRunCancellationCause", cause)
	}
	if cancellation != (protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: want}) {
		t.Fatalf("cancellation cause = %#v, want exact cleanup requirement %q", cancellation, want)
	}
}

func assertRevisionEventsOpen(t *testing.T, revisions <-chan RevisionEvent) {
	t.Helper()
	select {
	case _, ok := <-revisions:
		if !ok {
			t.Fatal("revision events channel was closed")
		}
	default:
	}
	select {
	case _, ok := <-revisions:
		if !ok {
			t.Fatal("revision events channel was closed")
		}
	default:
	}
}

type closedSessionCall struct {
	name string
	call func() error
}

func closedSessionCalls(t *testing.T, ctx context.Context, session *AuthenticationSession) []closedSessionCall {
	t.Helper()
	return []closedSessionCall{
		{name: "snapshot", call: func() error { _, err := session.Snapshot(ctx); return err }},
		{name: "network snapshot", call: func() error {
			_, err := session.ApplySystemNetworkSnapshot(ctx, usableSystemNetworkSnapshot(t, 2, "wifi", "Wi-Fi"))
			return err
		}},
		{name: "activate", call: func() error { _, err := session.Activate(ctx); return err }},
		{name: "suspend", call: func() error { _, err := session.Suspend(ctx); return err }},
		{name: "restart", call: func() error { _, err := session.Restart(ctx); return err }},
		{name: "replace", call: func() error {
			invalid := replacementRuntimeDefinition(t, &controlledFactory{})
			invalid.Configuration.AuthenticationSessionID = ""
			_, err := session.ReplaceRuntimeDefinition(ctx, invalid)
			return err
		}},
		{name: "shutdown", call: func() error { return session.Shutdown(ctx) }},
	}
}

func waitForAdmissionClosed(t *testing.T, ctx context.Context, session *AuthenticationSession) {
	t.Helper()
	for !session.closed.Load() {
		select {
		case <-ctx.Done():
			t.Fatalf("shutdown admission remained open: %v", ctx.Err())
		default:
		}
	}
}

func waitForAdmissionOpen(t *testing.T, ctx context.Context, session *AuthenticationSession) {
	t.Helper()
	for session.closed.Load() {
		select {
		case <-ctx.Done():
			t.Fatalf("shutdown admission remained closed: %v", ctx.Err())
		default:
		}
	}
}

func waitForInboxLength(t *testing.T, ctx context.Context, session *AuthenticationSession, want int) {
	t.Helper()
	for len(session.inbox) != want {
		select {
		case <-ctx.Done():
			t.Fatalf("inbox length = %d, want %d: %v", len(session.inbox), want, ctx.Err())
		default:
		}
	}
}
