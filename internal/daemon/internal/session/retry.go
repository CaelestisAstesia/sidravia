package session

import (
	"sync"
	"time"

	"sidravia/internal/connection/auth/protocol"
)

type RetryPolicy interface {
	Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool)
}

type RetryCancellation interface {
	Cancel()
}

type RetryScheduler interface {
	Schedule(time.Duration, func()) RetryCancellation
}

type defaultRetryPolicy struct{}

func NewDefaultRetryPolicy() RetryPolicy {
	return defaultRetryPolicy{}
}

func (defaultRetryPolicy) Delay(
	recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation,
	consecutiveFailures uint32,
) (time.Duration, bool) {
	switch recommendation {
	case protocol.RetryAfterStandardDelay:
		return 5 * time.Second, true
	case protocol.RetryAfterExtendedDelay:
		return 30 * time.Second, true
	case protocol.RetryAfterBoundedRecovery:
		if consecutiveFailures >= 1 && consecutiveFailures <= 3 {
			return (5 * time.Second) << (consecutiveFailures - 1), true
		}
		return 0, false
	default:
		return 0, false
	}
}

type timerRetryScheduler struct{}

func NewTimerRetryScheduler() RetryScheduler {
	return timerRetryScheduler{}
}

type timerRetryCancellation struct {
	once   sync.Once
	cancel func()
}

func (c *timerRetryCancellation) Cancel() {
	c.once.Do(c.cancel)
}

func (timerRetryScheduler) Schedule(delay time.Duration, callback func()) RetryCancellation {
	timer := time.AfterFunc(delay, callback)
	cancellation := &timerRetryCancellation{
		cancel: func() {
			timer.Stop()
		},
	}
	return cancellation
}

func (session *AuthenticationSession) handleRunFailure(code string, failure *protocol.AuthenticationProtocolRunFailure) {
	if session.consecutiveFailures < ^uint32(0) {
		session.consecutiveFailures++
	}
	if !session.definition.AutoReconnect {
		session.blockForAutomaticReconnectDisabled(failure)
		return
	}
	switch failure.HandlingRecommendation {
	case protocol.RetryAfterBoundedRecovery:
		session.retainRecoveryBudget = session.retainRecoveryBudget || failure.RetainRecoveryBudget
		if session.protocolRecoveryFailures < ^uint32(0) {
			session.protocolRecoveryFailures++
		}
		var delay time.Duration
		var ok bool
		if session.protocolRecoveryFailures <= 3 {
			delay, ok = session.retryPolicy.Delay(failure.HandlingRecommendation, session.protocolRecoveryFailures)
		}
		if !ok || delay <= 0 {
			// Session owns the exhausted budget. Preserve the original failure
			// code/cause, but publish the actual terminal handling recommendation.
			terminal := *failure
			terminal.HandlingRecommendation = protocol.BlockUntilExplicitRestartOrRelevantInputChange
			reason := code
			if session.protocolRecoveryFailures > 3 {
				reason = StateReasonCodeProtocolRecoveryExhausted
			}
			session.blockForFailure(reason, &terminal)
			return
		}
		session.scheduleRetry(delay, code, failure)
	case protocol.RetryAfterStandardDelay, protocol.RetryAfterExtendedDelay:
		delay, ok := session.retryPolicy.Delay(failure.HandlingRecommendation, session.consecutiveFailures)
		if !ok || delay <= 0 {
			session.blockForFailure(code, failure)
			return
		}
		session.scheduleRetry(delay, code, failure)
	case protocol.BlockUntilExplicitRestartOrRelevantInputChange:
		session.blockForFailure(code, failure)
	default:
		session.blockForFailure(code, failure)
	}
}

func (session *AuthenticationSession) scheduleRetry(delay time.Duration, code string, failure *protocol.AuthenticationProtocolRunFailure) {
	if session.bindingRecovery != nil {
		session.scheduleAutomaticRetry(delay, code, failure)
		return
	}
	session.invalidateRetrySchedule()
	scheduleID := session.retryScheduleID
	nextRetryAt := session.now().Add(delay)
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.State = WaitingBeforeRetry
		snapshot.StateReason = &StateReason{Code: code, Description: "Authentication protocol run failed; retry is scheduled."}
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = &nextRetryAt
		snapshot.LastAuthenticationFailure = session.publicFailure(failure)
	})
	session.diagnostics.RetryScheduled(nextRetryAt)
	session.retryCancellation = session.retryScheduler.Schedule(delay, func() {
		session.post(authenticationRetryDelayElapsedEvent{scheduleID: scheduleID})
	})
}

func (session *AuthenticationSession) handleRetryDelayElapsed(event authenticationRetryDelayElapsedEvent) {
	if session.bindingRecovery != nil {
		if event.scheduleID != session.retryScheduleID || session.retryCancellation == nil {
			return
		}
		session.clearAutomaticWake()
		session.reconcileAutomatic()
		return
	}
	if event.scheduleID != session.retryScheduleID || session.currentSnapshot.State != WaitingBeforeRetry {
		return
	}
	session.retryCancellation = nil
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.NextRetryAt = nil
	})
	session.startRunAfterRetryDelayIfNeeded()
}

func (session *AuthenticationSession) blockForFailure(code string, failure *protocol.AuthenticationProtocolRunFailure) {
	if session.bindingRecovery != nil {
		session.bindingRecovery.permitted = false
		session.bindingRecovery.retryAt = time.Time{}
		session.clearAutomaticWake()
	}
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.State = BlockedByError
		snapshot.StateReason = &StateReason{Code: code, Description: "Authentication protocol run failed."}
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
		snapshot.LastAuthenticationFailure = session.publicFailure(failure)
	})
}

func (session *AuthenticationSession) blockForAutomaticReconnectDisabled(failure *protocol.AuthenticationProtocolRunFailure) {
	if session.bindingRecovery != nil {
		session.bindingRecovery.permitted = false
		session.clearAutomaticWake()
	}
	session.automaticReconnectBlocked = true
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.State = BlockedByError
		snapshot.StateReason = &StateReason{Code: StateReasonCodeAutomaticReconnectDisabled, Description: "Automatic reconnect is disabled."}
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
		if failure != nil {
			snapshot.LastAuthenticationFailure = session.publicFailureWithRecommendation(
				failure,
				protocol.BlockUntilExplicitRestartOrRelevantInputChange,
			)
		}
	})
}

func (session *AuthenticationSession) resetRetryState() {
	session.consecutiveFailures = 0
	session.protocolRecoveryFailures = 0
	session.retainRecoveryBudget = false
	session.invalidateRetrySchedule()
}

func (session *AuthenticationSession) invalidateRetrySchedule() {
	session.invalidateRetryScheduleState()
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.NextRetryAt = nil
	})
}

func (session *AuthenticationSession) invalidateRetryScheduleState() {
	if session.retryCancellation != nil {
		session.retryCancellation.Cancel()
		session.retryCancellation = nil
	}
	session.retryScheduleID++
}
