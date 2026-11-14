package session

import (
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func serverResetFailure() *protocol.AuthenticationProtocolRunFailure {
	return &protocol.AuthenticationProtocolRunFailure{Code: "server_session_reset", Description: "The server reset this session.", HandlingRecommendation: protocol.RetryAfterBoundedRecovery, RetainRecoveryBudget: true}
}

func TestServerResetUsesExistingBoundedRecoveryAndHonorsPause(t *testing.T) {
	s, factory, scheduler := recoveryTestSession(t, true)
	ctx := testContext(t)
	for attempt := 0; attempt < 3; attempt++ {
		waitForFactoryRun(t, ctx, factory, attempt).unblock(serverResetFailure())
		snapshot := waitForState(t, ctx, s, WaitingBeforeRetry)
		if scheduler.delay(attempt) != (5*time.Second)<<attempt || snapshot.LastAuthenticationFailure.Code != "server_session_reset" {
			t.Fatal("reset bypassed existing recovery policy")
		}
		scheduler.callback(attempt)()
	}
	waitForFactoryRun(t, ctx, factory, 3).unblock(serverResetFailure())
	snapshot := waitForState(t, ctx, s, BlockedByError)
	if snapshot.StateReason.Code != StateReasonCodeProtocolRecoveryExhausted || scheduler.count() != 3 {
		t.Fatal("reset recovery was unbounded")
	}
	if _, err := s.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	waitForFactoryRun(t, ctx, factory, 4).unblock(serverResetFailure())
	_ = waitForState(t, ctx, s, WaitingBeforeRetry)
	if _, err := s.Suspend(ctx); err != nil {
		t.Fatal(err)
	}
	scheduler.callback(3)()
	_ = waitForState(t, ctx, s, Suspended)
	if len(factory.creationInputs()) != 5 {
		t.Fatal("paused reset recovery authenticated")
	}
}

func TestServerResetDoesNotOverrideDisabledAutoReconnect(t *testing.T) {
	s, factory, scheduler := recoveryTestSession(t, false)
	ctx := testContext(t)
	waitForFactoryRun(t, ctx, factory, 0).unblock(serverResetFailure())
	snapshot := waitForState(t, ctx, s, BlockedByError)
	if scheduler.count() != 0 || snapshot.NextRetryAt != nil || snapshot.LastAuthenticationFailure.Code != "server_session_reset" {
		t.Fatal("disabled reconnect was ignored")
	}
}

func TestEstablishedSessionsCannotReplenishServerResetRecovery(t *testing.T) {
	s, factory, scheduler := recoveryTestSession(t, true)
	ctx := testContext(t)
	for attempt := 0; attempt < 4; attempt++ {
		run := waitForFactoryRun(t, ctx, factory, attempt)
		if err := run.establish(ctx); err != nil {
			t.Fatal(err)
		}
		_ = waitForState(t, ctx, s, Authenticated)
		run.unblock(serverResetFailure())
		if attempt == 3 {
			break
		}
		_ = waitForState(t, ctx, s, WaitingBeforeRetry)
		if scheduler.delay(attempt) != (5*time.Second)<<attempt {
			t.Fatal("successful establishment replenished reset budget")
		}
		scheduler.callback(attempt)()
	}
	blocked := waitForState(t, ctx, s, BlockedByError)
	if blocked.StateReason.Code != StateReasonCodeProtocolRecoveryExhausted || blocked.LastAuthenticationFailure.Code != "server_session_reset" || scheduler.count() != 3 {
		t.Fatal("repeated established resets were unbounded")
	}
	if _, err := s.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	run := waitForFactoryRun(t, ctx, factory, 4)
	if err := run.establish(ctx); err != nil {
		t.Fatal(err)
	}
	_ = waitForState(t, ctx, s, Authenticated)
	run.unblock(serverResetFailure())
	_ = waitForState(t, ctx, s, WaitingBeforeRetry)
	if scheduler.delay(3) != 5*time.Second {
		t.Fatal("explicit restart did not reset budget")
	}
}

func TestNewSafeFailureCodesRemainStableWithCredentialSubstringCollisions(t *testing.T) {
	for _, code := range []string{"quota_exceeded", "account_suspended", "account_expired", "too_many_addresses", "server_session_reset", "server_session_ended"} {
		if got := safePublicFailureCode(code, []string{"account", "server", "quota", "addresses"}); got != code {
			t.Fatalf("static failure code changed: %s", got)
		}
	}
}

func TestAutomaticRestartAndInputChangeClearRetainedResetBudget(t *testing.T) {
	for _, action := range []string{"restart", "binding"} {
		t.Run(action, func(t *testing.T) {
			f := newRecoveryFixture(t, true, false, false)
			ctx := testContext(t)
			iface := recoveryInterface(t, "a")
			f.apply(t, 1, iface)
			run := f.run(t, 0, "a")
			if err := run.establish(ctx); err != nil {
				t.Fatal(err)
			}
			_ = waitForState(t, ctx, f.session, Authenticated)
			run.unblock(serverResetFailure())
			_ = waitForState(t, ctx, f.session, WaitingBeforeRetry)
			f.fire(t)
			f.run(t, 1, "a")
			if action == "restart" {
				if _, err := f.session.Restart(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				f.apply(t, 2, changedMAC(t, iface, 9))
			}
			for index := 2; index < 4; index++ {
				run = f.run(t, index, "a")
				if err := run.establish(ctx); err != nil {
					t.Fatal(err)
				}
				_ = waitForState(t, ctx, f.session, Authenticated)
				// Qualify the existing automatic round as stable, so its independent
				// launch backoff cannot mask the protocol recovery delay under test.
				f.advance(bindingStableSuccess + time.Second)
				run.unblock(protocolResponseFailure())
				_ = waitForState(t, ctx, f.session, WaitingBeforeRetry)
				if delay := f.scheduler.delay(f.scheduler.count() - 1); delay != 5*time.Second {
					t.Fatalf("%s retained reset policy after input reset: %v", action, delay)
				}
				if index == 2 {
					f.fire(t)
				}
			}
		})
	}
}
