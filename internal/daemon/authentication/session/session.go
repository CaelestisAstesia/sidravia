package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

// ErrAuthenticationSessionClosed is returned after shutdown begins rejecting public messages.
var ErrAuthenticationSessionClosed = errors.New("authentication session is closed")

type RevisionEvent struct {
	AuthenticationSessionID AuthenticationSessionID
	Revision                uint64
	Snapshot                Snapshot
}

// Diagnostics observes Session lifecycle events without altering Session
// behavior. The production adapter selects allowed fields and writes fixed
// Simplified Chinese messages; the sink has no return value and cannot change
// Session decisions, retry, state, IPC responses or return values. Diagnostic
// errors never propagate.
type Diagnostics interface {
	// SessionSnapshot records every committed public Snapshot revision at Info
	// level. It is invoked once per committed revision without coalescing, so
	// it is a more complete audit source than the coalescing RevisionEvents
	// stream.
	SessionSnapshot(snapshot Snapshot)
	// SessionCommand records a Session command at Debug level.
	SessionCommand(command string)
	// ProtocolRunGeneration records a new protocol-run generation at Debug
	// level.
	ProtocolRunGeneration(generation uint64)
	// RetryScheduled records a scheduled retry at Debug level.
	RetryScheduled(nextRetryAt time.Time)
}

// NoopDiagnostics is the explicit no-op implementation used when diagnostics
// are disabled, instead of scattered nil checks.
type NoopDiagnostics struct{}

func (NoopDiagnostics) SessionSnapshot(Snapshot)     {}
func (NoopDiagnostics) SessionCommand(string)        {}
func (NoopDiagnostics) ProtocolRunGeneration(uint64) {}
func (NoopDiagnostics) RetryScheduled(time.Time)     {}

// ProtocolDiagnosticsFactory builds a transport-neutral protocol diagnostics
// sink bound to one Session. The production adapter carries the SessionID so
// Trace datagram records can include it without the protocol run ever knowing
// the SessionID. A nil factory yields a no-op sink.
type ProtocolDiagnosticsFactory func(AuthenticationSessionID) protocol.AuthenticationProtocolDiagnostics

type Dependencies struct {
	Now                        func() time.Time
	RetryPolicy                RetryPolicy
	RetryScheduler             RetryScheduler
	Diagnostics                Diagnostics
	ProtocolDiagnosticsFactory ProtocolDiagnosticsFactory
}

type AuthenticationSession struct {
	business            BusinessSnapshot
	businessWake        chan struct{}
	businessMessageID   uint64
	definition          RuntimeDefinition
	now                 func() time.Time
	retryPolicy         RetryPolicy
	retryScheduler      RetryScheduler
	diagnostics         Diagnostics
	protocolDiagnostics protocol.AuthenticationProtocolDiagnostics

	inbox        chan sessionMessage
	privateInbox chan sessionMessage
	done         chan struct{}
	revisions    chan RevisionEvent
	admission    chan struct{}
	startOnce    sync.Once
	closed       atomic.Bool

	// The fields below are owned exclusively by run.
	currentSnapshot           Snapshot
	selector                  *networkBindingSelector
	bindingRecovery           *bindingRecovery // automatic binding recovery; nil for explicit bindings
	lastNetworkRevision       uint64
	hasNetworkRevision        bool
	desiredBinding            environment.SelectedSystemNetworkBinding
	hasDesiredBinding         bool
	active                    *activeProtocolRun
	nextGeneration            uint64
	consecutiveFailures       uint32
	protocolRecoveryFailures  uint32
	retainRecoveryBudget      bool
	retryScheduleID           uint64
	retryCancellation         RetryCancellation
	lastStopCleanupFailure    *protocol.AuthenticationProtocolRunFailure
	shuttingDown              bool
	automaticReconnectBlocked bool
	explicitRestartPending    bool
	shutdownReplies           []chan error
}

type activeProtocolRun struct {
	business       *businessMailbox
	generation     uint64
	run            protocol.AuthenticationProtocolRun
	context        context.Context
	cancel         context.CancelCauseFunc
	established    bool
	binding        bindingCandidate
	recoveryEpoch  uint64
	establishedAt  time.Time
	effectiveEndAt time.Time
}

func NewAuthenticationSession(
	definition RuntimeDefinition,
	initialIntent Intent,
	dependencies Dependencies,
) (*AuthenticationSession, error) {
	return NewAuthenticationSessionWithInitialRevision(definition, initialIntent, 1, dependencies)
}

func NewAuthenticationSessionWithInitialRevision(
	definition RuntimeDefinition,
	initialIntent Intent,
	initialRevision uint64,
	dependencies Dependencies,
) (*AuthenticationSession, error) {
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	if initialRevision == 0 {
		return nil, errors.New("authentication session revision is required")
	}
	definition = definition.Clone()
	return initializeAuthenticationSession(
		definition,
		initialIntent,
		initialRevision,
		dependencies,
	)
}

func initializeAuthenticationSession(
	definition RuntimeDefinition,
	initialIntent Intent,
	initialRevision uint64,
	dependencies Dependencies,
) (*AuthenticationSession, error) {
	if initialIntent != MaintainAuthentication && initialIntent != SuspendAuthentication {
		return nil, errors.New("unsupported authentication session intent")
	}
	if dependencies.RetryPolicy == nil {
		return nil, errors.New("authentication session retry policy is required")
	}
	if dependencies.RetryScheduler == nil {
		return nil, errors.New("authentication session retry scheduler is required")
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	configuration := definition.Configuration
	profileID := definition.InstitutionProfile.InstitutionProfileID
	profileDisplayName := definition.InstitutionProfile.DisplayName
	protocolID := definition.InstitutionProfile.AuthenticationProtocolID
	accountName := definition.AccountName()
	state := initialState(initialIntent)
	stateReason := initialStateReason(initialIntent, configuration.NetworkBindingPolicy)
	diagnostics := dependencies.Diagnostics
	if diagnostics == nil {
		diagnostics = NoopDiagnostics{}
	}
	var protocolDiagnostics protocol.AuthenticationProtocolDiagnostics = protocol.NoopAuthenticationProtocolDiagnostics{}
	if dependencies.ProtocolDiagnosticsFactory != nil {
		protocolDiagnostics = dependencies.ProtocolDiagnosticsFactory(configuration.AuthenticationSessionID)
	}
	admission := make(chan struct{}, 1)
	admission <- struct{}{}
	capabilities := protocol.BusinessCapabilities{}
	if provider, ok := definition.AuthenticationProtocolFactory.(protocol.BusinessCapabilityProvider); ok {
		var err error
		capabilities, err = provider.BusinessCapabilities(definition.InstitutionProfile.InstitutionProtocolConfiguration)
		if err != nil {
			return nil, err
		}
	}
	session := &AuthenticationSession{
		businessWake:        make(chan struct{}, 1),
		business:            BusinessSnapshot{SessionID: configuration.AuthenticationSessionID, Revision: 1, StatisticsSupported: capabilities.Statistics, TextSupported: capabilities.TextNotifications, Messages: []BusinessMessage{}, UpdatedAt: now()},
		definition:          definition,
		now:                 now,
		retryPolicy:         dependencies.RetryPolicy,
		retryScheduler:      dependencies.RetryScheduler,
		diagnostics:         diagnostics,
		protocolDiagnostics: protocolDiagnostics,
		inbox:               make(chan sessionMessage, 32),
		privateInbox:        make(chan sessionMessage, 1),
		done:                make(chan struct{}),
		revisions:           make(chan RevisionEvent, 1),
		admission:           admission,
		selector:            newPolicyBindingSelector(configuration.NetworkBindingPolicy),
		currentSnapshot: Snapshot{
			HasOverrideDocument:      len(configuration.ProtocolContextOverride) > 0,
			HasProtocolOverride:      protocol.HasOverrideFields(configuration.ProtocolContextOverride),
			ProtocolSocket:           ProtocolSocketObservation{State: ProtocolSocketNotObserved},
			AuthenticationSessionID:  configuration.AuthenticationSessionID,
			ConfigurationID:          configuration.ConfigurationID,
			DisplayName:              configuration.DisplayName,
			InstitutionProfileID:     profileID,
			InstitutionDisplayName:   profileDisplayName,
			AuthenticationProtocolID: protocolID,
			AccountName:              accountName,
			Intent:                   initialIntent,
			State:                    state,
			StateReason:              stateReason,
			Revision:                 initialRevision,
			UpdatedAt:                now(),
		},
	}
	if configuration.NetworkBindingPolicy.Mode == AutomaticallySelectLatestAvailable {
		session.bindingRecovery = &bindingRecovery{permitted: true}
		session.bindingRecovery.resetRound()
	}
	return session, nil
}

func initialState(intent Intent) State {
	if intent == SuspendAuthentication {
		return Suspended
	}
	return WaitingForNetwork
}

func initialStateReason(intent Intent, policies ...NetworkBindingPolicy) *StateReason {
	if intent == MaintainAuthentication {
		if len(policies) > 0 && policies[0].Mode == ExplicitInterfaceAndLocalIPv4 {
			return &StateReason{Code: StateReasonCodeNetworkBindingUnavailable, Description: "The selected network binding is unavailable."}
		}
		return &StateReason{Code: StateReasonCodeNetworkUnavailable, Description: "No usable network is available."}
	}
	return nil
}

func (session *AuthenticationSession) Start() {
	session.startOnce.Do(func() {
		go session.run()
	})
}

func (session *AuthenticationSession) RevisionEvents() <-chan RevisionEvent { return session.revisions }

func (session *AuthenticationSession) Snapshot(ctx context.Context) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.send(ctx, snapshotQuery{reply: reply}); err != nil {
		return Snapshot{}, err
	}
	return receiveSnapshotReply(ctx, session.done, reply)
}

func (session *AuthenticationSession) ApplySystemNetworkSnapshot(ctx context.Context, network environment.Snapshot) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.send(ctx, systemNetworkSnapshotCommand{network: network, reply: reply}); err != nil {
		return Snapshot{}, err
	}
	return receiveSnapshotReply(ctx, session.done, reply)
}

func (session *AuthenticationSession) Activate(ctx context.Context) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.send(ctx, activateCommand{reply: reply}); err != nil {
		return Snapshot{}, err
	}
	return receiveSnapshotReply(ctx, session.done, reply)
}

func (session *AuthenticationSession) Suspend(ctx context.Context) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.send(ctx, suspendCommand{reply: reply}); err != nil {
		return Snapshot{}, err
	}
	return receiveSnapshotReply(ctx, session.done, reply)
}

func (session *AuthenticationSession) Restart(ctx context.Context) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.send(ctx, restartCommand{reply: reply}); err != nil {
		return Snapshot{}, err
	}
	return receiveSnapshotReply(ctx, session.done, reply)
}

func (session *AuthenticationSession) Shutdown(ctx context.Context) error {
	reply := make(chan error, 1)
	if err := session.beginShutdown(ctx, shutdownCommand{reply: reply}); err != nil {
		return err
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
		return nil
	}
}

func receiveSnapshotReply(ctx context.Context, done <-chan struct{}, reply <-chan snapshotReply) (Snapshot, error) {
	select {
	case result := <-reply:
		return result.snapshot, result.err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-done:
		return Snapshot{}, ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) run() {
	defer close(session.revisions)
	session.publishInitialRevision()
	for {
		var message sessionMessage
		select {
		case message = <-session.privateInbox:
		default:
			select {
			case message = <-session.inbox:
			default:
				select {
				case message = <-session.privateInbox:
				case message = <-session.inbox:
				case <-session.businessWake:
					session.consumeBusiness()
					continue
				}
			}
		}
		switch message := message.(type) {
		case businessQuery:
			session.consumeBusiness()
			message.reply <- session.businessSnapshot()
		case snapshotQuery:
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
		case diagnosticSnapshotQuery:
			message.reply <- diagnosticSnapshotReply{snapshot: DiagnosticSnapshot{Snapshot: session.currentSnapshot.Clone(), ProtocolSocket: session.currentSnapshot.ProtocolSocket}}
		case protocolSocketOpenedEvent:
			session.handleProtocolSocketOpened(message)
		case protocolSocketClosedEvent:
			session.handleProtocolSocketClosed(message)
		case systemNetworkSnapshotCommand:
			session.diagnostics.SessionCommand("apply_network_snapshot")
			session.handleNetworkSnapshot(message.network)
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
		case activateCommand:
			session.diagnostics.SessionCommand("activate")
			session.handleActivate()
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
		case suspendCommand:
			session.diagnostics.SessionCommand("suspend")
			session.handleSuspend()
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
		case restartCommand:
			session.diagnostics.SessionCommand("restart")
			session.handleRestart()
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
		case shutdownCommand:
			session.diagnostics.SessionCommand("shutdown")
			session.handleShutdown(message.reply)
			if session.shuttingDown && session.active == nil {
				session.completeShutdown()
				return
			}
		case authenticationEstablishedEvent:
			session.handleAuthenticationEstablished(message)
		case authenticationProtocolRunFinishedEvent:
			session.handleProtocolRunFinished(message)
			if session.shuttingDown && session.active == nil {
				session.completeShutdown()
				return
			}
		case authenticationRetryDelayElapsedEvent:
			session.handleRetryDelayElapsed(message)
		case suspensionCompletedEvent:
			session.completeSuspensionIfReady()
		}
	}
}

func (session *AuthenticationSession) handleNetworkSnapshot(network environment.Snapshot) {
	if session.bindingRecovery != nil {
		session.handleAutomaticNetwork(network)
		return
	}
	if session.hasNetworkRevision && network.Revision <= session.lastNetworkRevision {
		return
	}
	session.hasNetworkRevision = true
	session.lastNetworkRevision = network.Revision
	binding, ok := session.selector.Select(network)
	if !ok {
		if session.hasDesiredBinding {
			session.resetRetryState()
		}
		session.hasDesiredBinding = false
		session.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.SelectedNetworkBinding = nil
			if snapshot.State == Stopping {
				return
			}
			if snapshot.Intent == MaintainAuthentication {
				snapshot.State = WaitingForNetwork
				snapshot.StateReason = initialStateReason(MaintainAuthentication, session.definition.Configuration.NetworkBindingPolicy)
				snapshot.AuthenticationEstablishedAt = nil
			}
		})
		session.cancelActive(protocol.TerminateWithoutLogout)
		return
	}

	changed := !session.hasDesiredBinding || !bindingsHaveEquivalentAuthenticationFacts(session.desiredBinding, binding)
	if changed {
		session.resetRetryState()
	}
	session.desiredBinding = binding
	session.hasDesiredBinding = true
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.SelectedNetworkBinding = bindingSummary(binding)
		if snapshot.State == Stopping {
			return
		}
		if changed && snapshot.Intent == MaintainAuthentication && session.active != nil && session.active.established {
			snapshot.State = Authenticating
			snapshot.StateReason = nil
			snapshot.AuthenticationEstablishedAt = nil
		}
	})
	if session.active != nil {
		if changed {
			session.cancelActive(protocol.TerminateWithoutLogout)
		}
		return
	}
	if session.explicitRestartPending {
		session.explicitRestartPending = false
		session.startRun(true)
		return
	}
	if session.currentSnapshot.Intent != MaintainAuthentication {
		return
	}
	if changed {
		session.startRunForRelevantInputChangeIfNeeded()
		return
	}
	if session.currentSnapshot.State != BlockedByError && session.currentSnapshot.State != WaitingBeforeRetry {
		session.startRunIfNeeded()
	}
}

func (session *AuthenticationSession) handleActivate() {
	if session.bindingRecovery != nil {
		if session.currentSnapshot.State == Stopping {
			return
		}
		if session.currentSnapshot.Intent == SuspendAuthentication {
			session.resetAutomaticIntent(true)
		}
		session.updateSnapshot(func(snapshot *Snapshot) { snapshot.Intent = MaintainAuthentication })
		session.reconcileAutomatic()
		return
	}
	if session.currentSnapshot.State == Stopping {
		return
	}
	session.updateSnapshot(func(snapshot *Snapshot) { snapshot.Intent = MaintainAuthentication })
	if !session.hasDesiredBinding {
		session.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.State = WaitingForNetwork
			snapshot.StateReason = initialStateReason(MaintainAuthentication, session.definition.Configuration.NetworkBindingPolicy)
		})
		return
	}
	if session.currentSnapshot.State == BlockedByError || session.currentSnapshot.State == WaitingBeforeRetry {
		return
	}
	session.startRunIfNeeded()
}

func (session *AuthenticationSession) handleSuspend() {
	if session.currentSnapshot.State == Stopping || session.currentSnapshot.State == Suspended {
		return
	}
	session.explicitRestartPending = false
	if session.bindingRecovery != nil {
		session.resetAutomaticIntent(false)
	}
	session.invalidateRetryScheduleState()
	session.lastStopCleanupFailure = nil
	hadActiveRun := session.active != nil
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.Intent = SuspendAuthentication
		snapshot.State = Stopping
		snapshot.StateReason = nil
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
	})
	session.cancelActive(protocol.TerminateWithBestEffortLogout)
	if !hadActiveRun {
		session.privateInbox <- suspensionCompletedEvent{}
	}
}

func (session *AuthenticationSession) handleRestart() {
	if session.bindingRecovery != nil {
		if session.currentSnapshot.State == Stopping {
			return
		}
		session.resetAutomaticIntent(true)
		session.consecutiveFailures, session.protocolRecoveryFailures = 0, 0
		session.retainRecoveryBudget = false
		session.automaticReconnectBlocked = false
		session.explicitRestartPending = true
		session.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.Intent = MaintainAuthentication
			if session.hasDesiredBinding {
				snapshot.State, snapshot.StateReason = Authenticating, nil
			} else {
				snapshot.State = WaitingForNetwork
				snapshot.StateReason = initialStateReason(MaintainAuthentication, session.definition.Configuration.NetworkBindingPolicy)
			}
			snapshot.AuthenticationEstablishedAt = nil
		})
		if session.active != nil {
			session.cancelActive(protocol.TerminateWithBestEffortLogout)
		} else {
			session.reconcileAutomatic()
		}
		return
	}
	if session.currentSnapshot.State == Stopping {
		return
	}
	session.explicitRestartPending = true
	session.resetRetryState()
	session.automaticReconnectBlocked = false
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.Intent = MaintainAuthentication
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
		if session.hasDesiredBinding {
			snapshot.State = Authenticating
			snapshot.StateReason = nil
			return
		}
		snapshot.State = WaitingForNetwork
		snapshot.StateReason = initialStateReason(MaintainAuthentication, session.definition.Configuration.NetworkBindingPolicy)
	})
	if session.active != nil {
		session.cancelActive(protocol.TerminateWithBestEffortLogout)
		return
	}
	if session.hasDesiredBinding {
		session.explicitRestartPending = false
		session.startRun(true)
	}
}

func (session *AuthenticationSession) handleShutdown(reply chan error) {
	if session.shuttingDown {
		session.shutdownReplies = append(session.shutdownReplies, reply)
		return
	}
	session.explicitRestartPending = false
	session.shuttingDown = true
	if session.bindingRecovery != nil {
		session.resetAutomaticIntent(false)
	}
	session.invalidateRetrySchedule()
	session.shutdownReplies = append(session.shutdownReplies, reply)
	session.cancelActive(protocol.TerminateWithBestEffortLogout)
}

func (session *AuthenticationSession) completeSuspensionIfReady() {
	if session.shuttingDown ||
		session.active != nil ||
		session.currentSnapshot.Intent != SuspendAuthentication ||
		session.currentSnapshot.State != Stopping {
		return
	}
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.State = Suspended
		snapshot.StateReason = nil
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
	})
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
		InstitutionProtocolConfiguration: append(protocol.InstitutionProtocolConfiguration(nil), session.definition.InstitutionProfile.InstitutionProtocolConfiguration...),
		AuthenticationCredential:         session.definition.AuthenticationCredential,
		SelectedSystemNetworkBinding:     session.desiredBinding,
		SystemHostInformation:            session.definition.SystemHostInformation,
		ProtocolContextOverride:          append(protocol.AuthenticationProtocolContextOverride(nil), session.definition.Configuration.ProtocolContextOverride...),
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

func (session *AuthenticationSession) completeShutdown() {
	close(session.done)
	for _, reply := range session.shutdownReplies {
		reply <- nil
	}
}

func bindingSummary(binding environment.SelectedSystemNetworkBinding) *NetworkBindingSummary {
	networkInterface := binding.NetworkInterface()
	return &NetworkBindingSummary{
		InterfaceID:      networkInterface.InterfaceID,
		DisplayName:      networkInterface.DisplayName,
		LocalIPv4Address: binding.LocalIPv4AddressAssignment().Address,
	}
}
