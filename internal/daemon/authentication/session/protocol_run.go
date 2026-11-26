package session

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	profilevalue "sidravia/internal/connection/profile"
	"sidravia/internal/daemon/authentication/protocol"
)

type protocolRunObserver struct {
	session    *AuthenticationSession
	generation uint64
	context    context.Context
	business   *businessMailbox
}

func (observer protocolRunObserver) AuthenticationEstablished() {
	observer.session.post(authenticationEstablishedEvent{generation: observer.generation})
}

func (observer protocolRunObserver) ProtocolSocketOpened(local, remote netip.AddrPort) error {
	if !validProtocolSocketEndpoint(local) || !validProtocolSocketEndpoint(remote) {
		return errors.New("authentication protocol socket endpoints are invalid")
	}
	observer.session.post(protocolSocketOpenedEvent{generation: observer.generation, local: local, remote: remote})
	return nil
}

func validProtocolSocketEndpoint(endpoint netip.AddrPort) bool {
	return endpoint.Addr().Is4() && !endpoint.Addr().IsUnspecified() && endpoint.Port() != 0
}

func (observer protocolRunObserver) ProtocolSocketClosed(closed bool) {
	observer.session.post(protocolSocketClosedEvent{generation: observer.generation, closed: closed})
}

func (session *AuthenticationSession) executeProtocolRun(run *activeProtocolRun) {
	var (
		failure  *protocol.AuthenticationProtocolRunFailure
		panicked any
	)
	func() {
		defer func() { panicked = recover() }()
		failure = run.run.Execute(run.context, protocolRunObserver{session: session, generation: run.generation, context: run.context, business: run.business})
	}()
	if panicked != nil {
		failure = &protocol.AuthenticationProtocolRunFailure{
			Code:                   protocol.AuthenticationProtocolFailureCode(StateReasonCodeProtocolContractViolated),
			Description:            "Authentication protocol run violated its contract.",
			HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange,
			DiagnosticCause:        fmt.Errorf("authentication protocol run panicked: %v", panicked),
		}
	}
	session.post(authenticationProtocolRunFinishedEvent{
		generation:        run.generation,
		failure:           failure,
		cancellationCause: context.Cause(run.context),
	})
}

func (session *AuthenticationSession) handleAuthenticationEstablished(event authenticationEstablishedEvent) {
	if session.active == nil || session.active.generation != event.generation || session.active.established || context.Cause(session.active.context) != nil {
		return
	}
	session.active.established = true
	session.businessChanged()
	session.active.establishedAt = session.now()
	if session.bindingRecovery != nil {
		session.bindingRecovery.retryAt = time.Time{}
	}
	session.consecutiveFailures = 0
	if !session.retainRecoveryBudget {
		session.protocolRecoveryFailures = 0
	}
	session.invalidateRetrySchedule()
	session.updateSnapshot(func(snapshot *Snapshot) {
		now := session.now()
		snapshot.State = Authenticated
		snapshot.StateReason = nil
		snapshot.AuthenticationEstablishedAt = &now
		snapshot.LastAuthenticationFailure = nil
		snapshot.NextRetryAt = nil
	})
}

func (session *AuthenticationSession) handleProtocolRunFinished(event authenticationProtocolRunFinishedEvent) {
	if session.active == nil || session.active.generation != event.generation {
		return
	}
	if session.currentSnapshot.ProtocolSocket.RunGeneration == event.generation && session.currentSnapshot.ProtocolSocket.State == ProtocolSocketOpen {
		session.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.ProtocolSocket.State = ProtocolSocketCloseUnconfirmed
			snapshot.ProtocolSocket.UpdatedAt = session.now()
		})
	}
	if cancellationCause := context.Cause(session.active.context); cancellationCause != nil {
		event.cancellationCause = cancellationCause
	}
	finished := session.active
	session.consumeBusiness()
	session.active = nil
	session.businessChanged()
	if session.bindingRecovery != nil && session.finishAutomaticRun(finished, event) {
		return
	}
	if event.cancellationCause != nil {
		var cancellation protocol.AuthenticationProtocolRunCancellationCause
		if errors.As(event.cancellationCause, &cancellation) &&
			cancellation.CleanupRequirement == protocol.TerminateWithBestEffortLogout {
			session.lastStopCleanupFailure = event.failure
		}
		if session.currentSnapshot.Intent == SuspendAuthentication {
			session.completeSuspensionIfReady()
			return
		}
		if !session.shuttingDown && session.explicitRestartPending {
			if session.hasDesiredBinding && session.currentSnapshot.Intent == MaintainAuthentication {
				session.explicitRestartPending = false
				session.startRun(true)
			}
			return
		}
		if !session.shuttingDown && session.currentSnapshot.Intent == MaintainAuthentication && session.hasDesiredBinding {
			if !session.definition.AutoReconnect && cancellation.CleanupRequirement == protocol.TerminateWithoutLogout {
				session.blockForAutomaticReconnectDisabled(event.failure)
				return
			}
			session.startRunIfNeeded()
		}
		return
	}
	if event.failure == nil {
		event.failure = &protocol.AuthenticationProtocolRunFailure{
			Code:                   protocol.AuthenticationProtocolFailureCode(StateReasonCodeProtocolContractViolated),
			Description:            "Authentication protocol run returned without establishing cancellation or a result.",
			HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange,
		}
		session.handleRunFailure(StateReasonCodeProtocolContractViolated, event.failure)
		return
	}
	reasonCode := StateReasonCodeProtocolRunFailed
	if event.failure.Code == protocol.AuthenticationProtocolFailureCode(StateReasonCodeProtocolContractViolated) {
		reasonCode = StateReasonCodeProtocolContractViolated
	}
	session.handleRunFailure(reasonCode, event.failure)
}

func (session *AuthenticationSession) startRunIfNeeded() {
	session.startRun(false)
}

func (session *AuthenticationSession) startRunForRelevantInputChangeIfNeeded() {
	session.startRun(true)
}

func (session *AuthenticationSession) startRunAfterRetryDelayIfNeeded() {
	session.startRun(true)
}

func (session *AuthenticationSession) startRun(allowRecoveryState bool) {
	if session.bindingRecovery != nil {
		session.reconcileAutomatic()
		return
	}
	session.createRun(allowRecoveryState)
}

// createRun is called only after automatic policy admission, or directly for
// the unchanged explicit-binding path.
func (session *AuthenticationSession) createRun(allowRecoveryState bool) {
	if session.shuttingDown || session.active != nil || session.currentSnapshot.Intent != MaintainAuthentication || !session.hasDesiredBinding || session.automaticReconnectBlocked {
		return
	}
	if !allowRecoveryState && (session.currentSnapshot.State == BlockedByError || session.currentSnapshot.State == WaitingBeforeRetry) {
		return
	}
	session.nextGeneration++
	session.beginBusinessRun(session.nextGeneration)
	generation := session.nextGeneration
	inputs := protocol.AuthenticationProtocolRunCreationInputs{
		InstitutionProtocolConfiguration: append(profilevalue.ProtocolConfiguration(nil), session.definition.InstitutionProfile.InstitutionProtocolConfiguration...),
		AuthenticationCredential:         session.definition.AuthenticationCredential,
		SelectedSystemNetworkBinding:     session.desiredBinding,
		SystemHostInformation:            session.definition.SystemHostInformation,
		ProtocolContextOverride:          append(profilevalue.ProtocolOverride(nil), session.definition.Configuration.ProtocolContextOverride...),
		Diagnostics:                      session.protocolDiagnostics,
	}
	run, err := session.definition.AuthenticationProtocolFactory.CreateAuthenticationProtocolRun(inputs)
	if err != nil || run == nil {
		var diagnosticCause error
		switch {
		case err != nil:
			diagnosticCause = err
		default:
			diagnosticCause = errors.New("authentication protocol factory returned a nil run")
		}
		failure := &protocol.AuthenticationProtocolRunFailure{
			Code:                   protocol.AuthenticationProtocolFailureCode(StateReasonCodeProtocolRunCreationFailed),
			Description:            "Unable to create authentication protocol run.",
			HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange,
			DiagnosticCause:        diagnosticCause,
		}
		session.blockForFailure(StateReasonCodeProtocolRunCreationFailed, failure)
		return
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	active := &activeProtocolRun{generation: generation, run: run, context: ctx, cancel: cancel, business: &businessMailbox{}}
	if session.bindingRecovery != nil {
		r := session.bindingRecovery
		active.binding = bindingCandidate{networkInterface: session.desiredBinding.NetworkInterface(), localAddress: session.desiredBinding.LocalIPv4AddressAssignment(), key: keyForBinding(session.desiredBinding)}
		active.recoveryEpoch = r.epoch
		r.starts++
		cursor := active.binding
		r.cursor = &cursor
		r.permitted = false
		r.retryAt = time.Time{}
		session.explicitRestartPending = false
	}
	session.active = active
	session.diagnostics.ProtocolRunGeneration(generation)
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.ProtocolSocket = ProtocolSocketObservation{RunGeneration: generation, State: ProtocolSocketNotObserved, UpdatedAt: session.now()}
		snapshot.State = Authenticating
		snapshot.StateReason = nil
		snapshot.AuthenticationEstablishedAt = nil
	})
	go session.executeProtocolRun(active)
}

func (session *AuthenticationSession) cancelActive(requirement protocol.AuthenticationProtocolRunCleanupRequirement) {
	if session.active == nil || context.Cause(session.active.context) != nil {
		return
	}
	session.active.effectiveEndAt = session.now()
	session.active.cancel(protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: requirement})
	session.businessChanged()
}
