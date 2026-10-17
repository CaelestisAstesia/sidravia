package session

import (
	"errors"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

const (
	bindingStabilityDelay = 4 * time.Second
	bindingFailureLimit   = 2
	bindingRoundLimit     = 4
	bindingRoundDelay     = 30 * time.Second
	bindingStableSuccess  = 30 * time.Second
)

// All fields belong to the Session actor. No history survives the Session.
type bindingRecovery struct {
	epoch     uint64
	starts    uint8
	failures  map[environment.InterfaceID]uint8
	roundAt   time.Time
	retryAt   time.Time
	wakeAt    time.Time
	cursor    *bindingCandidate
	permitted bool
}

func (r *bindingRecovery) resetRound() {
	r.epoch++
	r.starts = 0
	r.failures = make(map[environment.InterfaceID]uint8)
	r.roundAt = time.Time{}
}

func (s *AuthenticationSession) resetAutomaticIntent(permitted bool) {
	r := s.automatic
	r.resetRound()
	r.retryAt = time.Time{}
	r.wakeAt = time.Time{}
	r.permitted = permitted
	s.invalidateRetrySchedule()
}

func (s *AuthenticationSession) handleAutomaticNetwork(network environment.Snapshot) {
	if s.hasNetworkRevision && network.Revision <= s.lastNetworkRevision {
		return
	}
	s.hasNetworkRevision, s.lastNetworkRevision = true, network.Revision
	now := s.now()
	s.selector.observe(network, now)
	candidate, _ := s.selector.choose(now, nil, nil, false)
	changed := false
	if candidate == nil {
		changed = s.hasDesiredBinding
		s.hasDesiredBinding = false

	} else {
		binding := s.selector.commit(*candidate)
		changed = !s.hasDesiredBinding || !bindingsHaveEquivalentAuthenticationFacts(s.desiredBinding, binding)
		s.desiredBinding, s.hasDesiredBinding = binding, true

	}
	if changed {
		// Real input changes retain the existing recovery qualification, but never
		// replenish the new launch budget or discard an unelapsed retry deadline.
		s.consecutiveFailures, s.protocolRecoveryFailures = 0, 0
		s.automatic.permitted = true
		if s.active != nil {
			s.cancelActive(protocol.TerminateWithoutLogout)
		}
	}
	s.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.SelectedNetworkBinding = nil
		if s.hasDesiredBinding {
			snapshot.SelectedNetworkBinding = bindingSummary(s.desiredBinding)
		}
		if snapshot.State == Stopping || snapshot.Intent != MaintainAuthentication {
			return
		}
		if !s.hasDesiredBinding && s.automatic.permitted {
			snapshot.State = WaitingForNetwork
			snapshot.StateReason = initialStateReason(MaintainAuthentication, s.definition.Configuration.NetworkBindingPolicy)
			snapshot.AuthenticationEstablishedAt, snapshot.NextRetryAt = nil, nil
		} else if changed && s.active != nil && s.active.established {
			snapshot.State, snapshot.StateReason = Authenticating, nil
			snapshot.AuthenticationEstablishedAt = nil
		}
	})
	if s.currentSnapshot.State == Stopping || s.currentSnapshot.Intent != MaintainAuthentication {
		return
	}

	s.reconcileAutomatic()
}

// reconcileAutomatic is the only automatic launch gate. Time-only wakeups use
// saved facts and cannot grant permission to leave a blocking failure.
func (s *AuthenticationSession) reconcileAutomatic() {
	r := s.automatic
	if s.shuttingDown || s.currentSnapshot.Intent != MaintainAuthentication || s.currentSnapshot.State == Stopping || s.active != nil || s.automaticReconnectBlocked || !r.permitted {
		return
	}
	now := s.now()
	candidate, readyAt := s.selector.choose(now, r.failures, r.cursor, false)
	exhausted := len(s.selector.candidates) > 0 && candidate == nil && readyAt.IsZero()
	if (r.starts >= bindingRoundLimit || exhausted) && r.roundAt.IsZero() {
		r.roundAt = now.Add(bindingRoundDelay)
	}
	if !r.roundAt.IsZero() {
		// Select the candidate that would be retried in the next round. Its
		// eligibility does not clear this round's counters before all gates pass.
		rotate := r.cursor != nil && r.failures[r.cursor.key.interfaceID] >= bindingFailureLimit
		candidate, readyAt = s.selector.choose(now, nil, r.cursor, rotate)
		if !r.roundAt.After(now) && !r.retryAt.After(now) && candidate != nil && !readyAt.After(now) {
			r.resetRound()
		}
	}
	if candidate == nil {
		s.hasDesiredBinding = false
		s.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.State = WaitingForNetwork
			snapshot.StateReason = initialStateReason(MaintainAuthentication, s.definition.Configuration.NetworkBindingPolicy)
			snapshot.SelectedNetworkBinding, snapshot.AuthenticationEstablishedAt, snapshot.NextRetryAt = nil, nil, nil
		})
		s.clearAutomaticWake()
		return
	}
	deadline := latestTime(readyAt, r.retryAt, r.roundAt)
	if deadline.After(now) {
		realRetry := !readyAt.After(now)
		if realRetry {
			binding := s.selector.commit(*candidate)
			s.desiredBinding, s.hasDesiredBinding = binding, true
			s.updateSnapshot(func(snapshot *Snapshot) {
				snapshot.SelectedNetworkBinding = bindingSummary(binding)
				snapshot.State = WaitingBeforeRetry
				snapshot.AuthenticationEstablishedAt = nil
				if snapshot.LastAuthenticationFailure == nil {
					snapshot.StateReason = nil
				}
			})
		}
		if !realRetry {
			binding := s.selector.commit(*candidate)
			s.desiredBinding, s.hasDesiredBinding = binding, true
			s.updateSnapshot(func(snapshot *Snapshot) {
				snapshot.State = WaitingForNetwork
				snapshot.StateReason = initialStateReason(MaintainAuthentication, s.definition.Configuration.NetworkBindingPolicy)
				snapshot.SelectedNetworkBinding = bindingSummary(binding)
				snapshot.AuthenticationEstablishedAt = nil
			})
		}
		s.waitAutomatic(deadline, realRetry)
		return
	}
	binding := s.selector.commit(*candidate)
	s.desiredBinding, s.hasDesiredBinding = binding, true
	s.updateSnapshot(func(snapshot *Snapshot) { snapshot.SelectedNetworkBinding = bindingSummary(binding) })
	s.clearAutomaticWake()
	s.createRun(true)
}

func latestTime(values ...time.Time) time.Time {
	var result time.Time
	for _, value := range values {
		if value.After(result) {
			result = value
		}
	}
	return result
}

func (s *AuthenticationSession) clearAutomaticWake() {
	s.invalidateRetrySchedule()
	s.automatic.wakeAt = time.Time{}
}

func (s *AuthenticationSession) waitAutomatic(deadline time.Time, realRetry bool) {
	r := s.automatic
	s.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.NextRetryAt = nil
		if realRetry {
			at := deadline
			snapshot.NextRetryAt = &at
		}
	})
	if s.retryCancellation != nil && r.wakeAt.Equal(deadline) {
		return
	}
	s.invalidateRetryScheduleState()
	r.wakeAt = deadline
	scheduleID := s.retryScheduleID
	if realRetry {
		s.diagnostics.RetryScheduled(deadline)
	}
	s.retryCancellation = s.retryScheduler.Schedule(deadline.Sub(s.now()), func() {
		s.post(authenticationRetryDelayElapsedEvent{scheduleID: scheduleID})
	})
}

func (s *AuthenticationSession) scheduleAutomaticRetry(delay time.Duration, code string, failure *protocol.AuthenticationProtocolRunFailure) {
	r := s.automatic
	r.permitted = true
	r.retryAt = s.now().Add(delay)
	s.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.State = WaitingBeforeRetry
		snapshot.StateReason = &StateReason{Code: code, Description: "Authentication protocol run failed; retry is scheduled."}
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.LastAuthenticationFailure = s.publicFailure(failure)
	})
	s.reconcileAutomatic()
}

// The common finish handler has already checked Run generation and socket
// observations. Return true when cancellation has been fully handled here.
func (s *AuthenticationSession) finishAutomaticRun(run *activeProtocolRun, event authenticationProtocolRunFinishedEvent) bool {
	r := s.automatic
	sameEpoch := run.recoveryEpoch == r.epoch
	end := run.effectiveEndAt
	if end.IsZero() {
		end = s.now()
	}
	if sameEpoch && run.established && end.Sub(run.establishedAt) >= bindingStableSuccess {
		r.resetRound()
	}
	if event.cancellationCause != nil {
		var cancellation protocol.AuthenticationProtocolRunCancellationCause
		errors.As(event.cancellationCause, &cancellation)
		if cancellation.CleanupRequirement == protocol.TerminateWithBestEffortLogout {
			s.lastStopCleanupFailure = event.failure
		}
		if s.currentSnapshot.Intent == SuspendAuthentication {
			s.completeSuspensionIfReady()
			return true
		}
		if s.shuttingDown {
			return true
		}
		if sameEpoch {
			r.permitted = true
		}
		if s.explicitRestartPending {
			r.permitted = true
		} else if !s.definition.AutoReconnect {
			s.blockForAutomaticReconnectDisabled(event.failure)
			return true
		}
		s.reconcileAutomatic()
		return true
	}
	// A natural failure never uses a different desired binding for attribution.
	r.permitted = false
	if sameEpoch && event.failure != nil && event.failure.HandlingRecommendation == protocol.RetryAfterStandardDelay {
		id := run.binding.key.interfaceID
		if r.failures[id] < bindingFailureLimit {
			r.failures[id]++
		}
	}
	return false
}
