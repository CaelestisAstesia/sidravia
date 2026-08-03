package session

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

// ErrAuthenticationSessionClosed is returned after shutdown begins rejecting public messages.
var ErrAuthenticationSessionClosed = errors.New("authentication session is closed")

type RevisionEvent struct {
	AuthenticationSessionID AuthenticationSessionID
	Revision                uint64
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
	definition                 RuntimeDefinition
	runtimeDefinitionAvailable bool
	now                        func() time.Time
	retryPolicy                RetryPolicy
	retryScheduler             RetryScheduler
	diagnostics                Diagnostics
	protocolDiagnostics        protocol.AuthenticationProtocolDiagnostics

	inbox        chan sessionMessage
	privateInbox chan sessionMessage
	done         chan struct{}
	revisions    chan RevisionEvent
	admission    chan struct{}
	startOnce    sync.Once
	closed       atomic.Bool

	// The fields below are owned exclusively by run.
	currentSnapshot           Snapshot
	selector                  *automaticBindingSelector
	lastNetworkRevision       uint64
	hasNetworkRevision        bool
	desiredBinding            environment.SelectedSystemNetworkBinding
	hasDesiredBinding         bool
	active                    *activeProtocolRun
	nextGeneration            uint64
	consecutiveFailures       uint32
	retryScheduleID           uint64
	retryCancellation         RetryCancellation
	lastStopCleanupFailure    *protocol.AuthenticationProtocolRunFailure
	shuttingDown              bool
	automaticReconnectBlocked bool
	shutdownReplies           []chan error
}

type activeProtocolRun struct {
	generation  uint64
	run         protocol.AuthenticationProtocolRun
	context     context.Context
	cancel      context.CancelCauseFunc
	established bool
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
		true,
		unresolvedRuntimeDefinition{},
		initialIntent,
		initialRevision,
		dependencies,
	)
}

type unresolvedRuntimeDefinition struct {
	Configuration            Configuration
	ProfileDisplayName       string
	AuthenticationProtocolID protocol.AuthenticationProtocolID
	AccountName              string
}

func (definition unresolvedRuntimeDefinition) Clone() unresolvedRuntimeDefinition {
	cloned := definition
	cloned.Configuration.ProtocolContextOverride = cloneProtocolContextOverride(
		definition.Configuration.ProtocolContextOverride,
	)
	return cloned
}

func newUnresolvedAuthenticationSession(
	definition unresolvedRuntimeDefinition,
	initialIntent Intent,
	initialRevision uint64,
	dependencies Dependencies,
) (*AuthenticationSession, error) {
	definition = definition.Clone()
	if definition.Configuration.AuthenticationSessionID == "" {
		return nil, errors.New("authentication session ID is required")
	}
	if initialRevision == 0 {
		return nil, errors.New("authentication session revision is required")
	}
	return initializeAuthenticationSession(
		RuntimeDefinition{},
		false,
		definition,
		initialIntent,
		initialRevision,
		dependencies,
	)
}

func initializeAuthenticationSession(
	definition RuntimeDefinition,
	runtimeDefinitionAvailable bool,
	unresolved unresolvedRuntimeDefinition,
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
	stateReason := initialStateReason(initialIntent)
	if !runtimeDefinitionAvailable {
		configuration = unresolved.Configuration
		profileID = unresolved.Configuration.InstitutionProfileID
		profileDisplayName = unresolved.ProfileDisplayName
		protocolID = unresolved.AuthenticationProtocolID
		accountName = unresolved.AccountName
		state = BlockedByError
		stateReason = runtimeDefinitionUnavailableReason()
	}
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
	session := &AuthenticationSession{
		definition:                 definition,
		runtimeDefinitionAvailable: runtimeDefinitionAvailable,
		now:                        now,
		retryPolicy:                dependencies.RetryPolicy,
		retryScheduler:             dependencies.RetryScheduler,
		diagnostics:                diagnostics,
		protocolDiagnostics:        protocolDiagnostics,
		inbox:                      make(chan sessionMessage, 32),
		privateInbox:               make(chan sessionMessage, 1),
		done:                       make(chan struct{}),
		revisions:                  make(chan RevisionEvent, 1),
		admission:                  admission,
		selector:                   newAutomaticBindingSelector(),
		currentSnapshot: Snapshot{
			AuthenticationSessionID:  configuration.AuthenticationSessionID,
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
	return session, nil
}

func runtimeDefinitionUnavailableReason() *StateReason {
	return &StateReason{
		Code:        StateReasonCodeRuntimeDefinitionUnavailable,
		Description: "Authentication session configuration is unavailable.",
	}
}

func initialState(intent Intent) State {
	if intent == SuspendAuthentication {
		return Suspended
	}
	return WaitingForNetwork
}

func initialStateReason(intent Intent) *StateReason {
	if intent == MaintainAuthentication {
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

func (session *AuthenticationSession) snapshotForWatcher(ctx context.Context) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.sendFromWatcher(ctx, snapshotQuery{reply: reply}); err != nil {
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

func (session *AuthenticationSession) ReplaceRuntimeDefinition(ctx context.Context, definition RuntimeDefinition) (Snapshot, error) {
	if session.closed.Load() {
		return Snapshot{}, ErrAuthenticationSessionClosed
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	definition = definition.Clone()
	if err := definition.Validate(); err != nil {
		return Snapshot{}, err
	}
	return session.replaceRuntimeDefinitionWithMessage(ctx, runtimeDefinitionReplacement{definition: &definition})
}

func (session *AuthenticationSession) replaceRuntimeDefinitionForSupervisor(ctx context.Context, definition RuntimeDefinition) (Snapshot, error) {
	if session.closed.Load() {
		return Snapshot{}, ErrAuthenticationSessionClosed
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	definition = definition.Clone()
	if err := definition.Validate(); err != nil {
		return Snapshot{}, err
	}
	return session.replaceRuntimeDefinitionWithMessage(ctx, runtimeDefinitionReplacement{definition: &definition, preserveSessionID: true})
}

func (session *AuthenticationSession) replaceUnresolvedRuntimeDefinition(ctx context.Context, definition unresolvedRuntimeDefinition) (Snapshot, error) {
	if session.closed.Load() {
		return Snapshot{}, ErrAuthenticationSessionClosed
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	definition = definition.Clone()
	if definition.Configuration.AuthenticationSessionID == "" {
		return Snapshot{}, errors.New("authentication session ID is required")
	}
	return session.replaceRuntimeDefinitionWithMessage(ctx, runtimeDefinitionReplacement{unresolved: &definition, preserveSessionID: true})
}

func (session *AuthenticationSession) replaceRuntimeDefinitionWithMessage(ctx context.Context, replacement runtimeDefinitionReplacement) (Snapshot, error) {
	reply := make(chan snapshotReply, 1)
	if err := session.send(ctx, replaceRuntimeDefinitionCommand{replacement: replacement.clone(), reply: reply}); err != nil {
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

func (session *AuthenticationSession) beginDeletionPreparation(
	ctx context.Context,
	preparation *shutdownPreparation,
) error {
	if preparation == nil {
		return errors.New("shutdown preparation is required")
	}
	return session.beginShutdown(ctx, shutdownCommand{preparation: preparation})
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
	session.publishInitialRevision()
	for {
		var message sessionMessage
		select {
		case message = <-session.privateInbox:
		case message = <-session.inbox:
		}
		switch message := message.(type) {
		case snapshotQuery:
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
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
		case replaceRuntimeDefinitionCommand:
			session.diagnostics.SessionCommand("replace_runtime_definition")
			if !message.replacement.valid() {
				message.reply <- snapshotReply{err: errors.New("runtime definition replacement is invalid")}
				continue
			}
			var replacementSessionID AuthenticationSessionID
			if message.replacement.definition != nil {
				replacementSessionID = message.replacement.definition.Configuration.AuthenticationSessionID
			} else {
				replacementSessionID = message.replacement.unresolved.Configuration.AuthenticationSessionID
			}
			if message.replacement.preserveSessionID && replacementSessionID != session.currentSnapshot.AuthenticationSessionID {
				message.reply <- snapshotReply{err: errors.New("authentication session ID cannot be replaced")}
				continue
			}
			if message.replacement.definition != nil {
				session.handleReplaceRuntimeDefinition(*message.replacement.definition)
			} else {
				session.handleReplaceUnresolvedRuntimeDefinition(*message.replacement.unresolved)
			}
			message.reply <- snapshotReply{snapshot: session.currentSnapshot.Clone()}
		case shutdownCommand:
			session.diagnostics.SessionCommand("shutdown")
			if message.preparation != nil {
				decision := session.handleShutdownPreparation(message.preparation)
				if decision == shutdownPreparationAbort {
					continue
				}
			} else {
				session.handleShutdown(message.reply)
			}
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
			if !session.runtimeDefinitionAvailable {
				forceRuntimeDefinitionUnavailable(snapshot)
				return
			}
			if snapshot.Intent == MaintainAuthentication {
				snapshot.State = WaitingForNetwork
				snapshot.StateReason = &StateReason{Code: StateReasonCodeNetworkUnavailable, Description: "No usable network is available."}
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
		if !session.runtimeDefinitionAvailable {
			forceRuntimeDefinitionUnavailable(snapshot)
			return
		}
		if changed && snapshot.Intent == MaintainAuthentication && session.active != nil && session.active.established {
			snapshot.State = Authenticating
			snapshot.StateReason = nil
			snapshot.AuthenticationEstablishedAt = nil
		}
	})
	if !session.runtimeDefinitionAvailable {
		return
	}
	if session.active != nil {
		if changed {
			session.cancelActive(protocol.TerminateWithoutLogout)
		}
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
	if session.currentSnapshot.State == Stopping {
		return
	}
	session.updateSnapshot(func(snapshot *Snapshot) { snapshot.Intent = MaintainAuthentication })
	if !session.runtimeDefinitionAvailable {
		session.updateSnapshot(forceRuntimeDefinitionUnavailable)
		return
	}
	if !session.hasDesiredBinding {
		session.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.State = WaitingForNetwork
			snapshot.StateReason = &StateReason{Code: StateReasonCodeNetworkUnavailable, Description: "No usable network is available."}
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
	if session.currentSnapshot.State == Stopping {
		return
	}
	session.resetRetryState()
	session.automaticReconnectBlocked = false
	if !session.runtimeDefinitionAvailable {
		session.updateSnapshot(func(snapshot *Snapshot) {
			snapshot.Intent = MaintainAuthentication
			forceRuntimeDefinitionUnavailable(snapshot)
		})
		return
	}
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
		snapshot.StateReason = &StateReason{Code: StateReasonCodeNetworkUnavailable, Description: "No usable network is available."}
	})
	if session.active != nil {
		session.cancelActive(protocol.TerminateWithBestEffortLogout)
		return
	}
	session.startRunIfNeeded()
}

func (session *AuthenticationSession) handleReplaceRuntimeDefinition(definition RuntimeDefinition) {
	session.resetRetryState()
	hadActiveRun := session.active != nil
	wasStopping := session.currentSnapshot.State == Stopping
	session.definition = definition.Clone()
	session.runtimeDefinitionAvailable = true
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.AuthenticationSessionID = definition.Configuration.AuthenticationSessionID
		snapshot.DisplayName = definition.Configuration.DisplayName
		snapshot.InstitutionProfileID = definition.InstitutionProfile.InstitutionProfileID
		snapshot.InstitutionDisplayName = definition.InstitutionProfile.DisplayName
		snapshot.AuthenticationProtocolID = definition.InstitutionProfile.AuthenticationProtocolID
		snapshot.AccountName = definition.AccountName()
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
		switch {
		case wasStopping:
			snapshot.State = Stopping
			snapshot.StateReason = nil
		case snapshot.Intent == SuspendAuthentication:
			snapshot.State = Suspended
			snapshot.StateReason = nil
		case hadActiveRun:
			snapshot.State = Authenticating
			snapshot.StateReason = nil
		case !session.hasDesiredBinding:
			snapshot.State = WaitingForNetwork
			snapshot.StateReason = &StateReason{Code: StateReasonCodeNetworkUnavailable, Description: "No usable network is available."}
		}
	})
	if session.active != nil {
		session.cancelActive(protocol.TerminateWithBestEffortLogout)
		return
	}
	session.startRunForRelevantInputChangeIfNeeded()
}

func (session *AuthenticationSession) handleReplaceUnresolvedRuntimeDefinition(definition unresolvedRuntimeDefinition) {
	session.resetRetryState()
	wasStopping := session.currentSnapshot.State == Stopping
	session.runtimeDefinitionAvailable = false
	session.definition = RuntimeDefinition{}
	definition = definition.Clone()
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.DisplayName = definition.Configuration.DisplayName
		snapshot.InstitutionProfileID = definition.Configuration.InstitutionProfileID
		snapshot.InstitutionDisplayName = definition.ProfileDisplayName
		snapshot.AuthenticationProtocolID = definition.AuthenticationProtocolID
		snapshot.AccountName = definition.AccountName
		snapshot.LastAuthenticationFailure = nil
		if wasStopping {
			snapshot.State = Stopping
			snapshot.StateReason = nil
			snapshot.AuthenticationEstablishedAt = nil
			snapshot.NextRetryAt = nil
			return
		}
		forceRuntimeDefinitionUnavailable(snapshot)
	})
	session.cancelActive(protocol.TerminateWithBestEffortLogout)
}

func forceRuntimeDefinitionUnavailable(snapshot *Snapshot) {
	snapshot.State = BlockedByError
	snapshot.StateReason = runtimeDefinitionUnavailableReason()
	snapshot.AuthenticationEstablishedAt = nil
	snapshot.NextRetryAt = nil
}

func (session *AuthenticationSession) handleShutdown(reply chan error) {
	if session.shuttingDown {
		session.shutdownReplies = append(session.shutdownReplies, reply)
		return
	}
	session.shuttingDown = true
	session.invalidateRetrySchedule()
	session.shutdownReplies = append(session.shutdownReplies, reply)
	session.cancelActive(protocol.TerminateWithBestEffortLogout)
}

func (session *AuthenticationSession) handleShutdownPreparation(preparation *shutdownPreparation) shutdownPreparationDecision {
	preparation.prepared <- session.currentSnapshot.Clone()
	decision := <-preparation.decision
	if decision == shutdownPreparationAbort {
		session.closed.Store(false)
		close(preparation.decisionAck)
		return decision
	}
	session.shuttingDown = true
	session.invalidateRetrySchedule()
	session.cancelActive(protocol.TerminateWithBestEffortLogout)
	close(preparation.decisionAck)
	return decision
}

func (session *AuthenticationSession) handleAuthenticationEstablished(event authenticationEstablishedEvent) {
	if session.active == nil || session.active.generation != event.generation || session.active.established || context.Cause(session.active.context) != nil {
		return
	}
	session.active.established = true
	session.resetRetryState()
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
	if cancellationCause := context.Cause(session.active.context); cancellationCause != nil {
		event.cancellationCause = cancellationCause
	}
	session.active = nil
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
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.State = BlockedByError
		snapshot.StateReason = &StateReason{Code: code, Description: "Authentication protocol run failed."}
		snapshot.AuthenticationEstablishedAt = nil
		snapshot.NextRetryAt = nil
		snapshot.LastAuthenticationFailure = session.publicFailure(failure)
	})
}

func (session *AuthenticationSession) blockForAutomaticReconnectDisabled(failure *protocol.AuthenticationProtocolRunFailure) {
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

// publicFailure projects a protocol failure using its recommendation when the
// Session applies that recommendation unchanged.
func (session *AuthenticationSession) publicFailure(failure *protocol.AuthenticationProtocolRunFailure) *AuthenticationFailure {
	return session.publicFailureWithRecommendation(failure, failure.HandlingRecommendation)
}

// publicFailureWithRecommendation projects the safe protocol failure code and
// description together with the effective recommendation selected by Session
// policy. The input protocol failure remains unchanged for Session decisions.
func (session *AuthenticationSession) publicFailureWithRecommendation(
	failure *protocol.AuthenticationProtocolRunFailure,
	recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation,
) *AuthenticationFailure {
	forbidden := session.publicFailureForbiddenMaterial(failure)
	return &AuthenticationFailure{
		Code:                   protocol.AuthenticationProtocolFailureCode(safePublicFailureCode(string(failure.Code), forbidden)),
		Description:            safePublicFailureDescription(failure.Description, forbidden),
		HandlingRecommendation: safePublicFailureRecommendation(recommendation, forbidden),
	}
}

func (session *AuthenticationSession) publicFailureForbiddenMaterial(failure *protocol.AuthenticationProtocolRunFailure) []string {
	values := []string{
		session.definition.AuthenticationCredential.Username,
		session.definition.AuthenticationCredential.Password,
		"",
		string(session.definition.Configuration.ProtocolContextOverride),
		string(session.definition.InstitutionProfile.InstitutionProtocolConfiguration),
	}
	if failure.DiagnosticCause != nil {
		values = append(values, diagnosticCauseText(failure.DiagnosticCause))
	}
	forbidden := values[:0]
	for _, value := range values {
		if value != "" {
			forbidden = append(forbidden, value)
		}
	}
	return forbidden
}

func diagnosticCauseText(cause error) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	return cause.Error()
}

func safePublicFailureCode(value string, forbidden []string) string {
	if validPublicFailureCode(value) && !containsForbiddenPublicFailureMaterial(value, forbidden) {
		return value
	}
	return firstSafePublicFailureFallback(forbidden,
		"authentication_protocol_failure",
		"protocol_failure",
		"failure",
	)
}

func validPublicFailureCode(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func safePublicFailureDescription(value string, forbidden []string) string {
	if validPublicFailureDescription(value) && !containsForbiddenPublicFailureMaterial(value, forbidden) {
		return value
	}
	return firstSafePublicFailureFallback(forbidden,
		"Authentication protocol failure.",
		"Protocol failure.",
		"Authentication failed.",
	)
}

func validPublicFailureDescription(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 256 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func firstSafePublicFailureFallback(forbidden []string, fallbacks ...string) string {
	for _, fallback := range fallbacks {
		if !containsForbiddenPublicFailureMaterial(fallback, forbidden) {
			return fallback
		}
	}
	return ""
}

func containsForbiddenPublicFailureMaterial(value string, forbidden []string) bool {
	for _, material := range forbidden {
		if material != "" && strings.Contains(value, material) {
			return true
		}
	}
	return false
}

func safePublicFailureRecommendation(
	value protocol.AuthenticationProtocolFailureHandlingRecommendation,
	forbidden []string,
) protocol.AuthenticationProtocolFailureHandlingRecommendation {
	if validPublicFailureRecommendation(value) && !containsForbiddenPublicFailureMaterial(string(value), forbidden) {
		return value
	}
	for _, fallback := range []protocol.AuthenticationProtocolFailureHandlingRecommendation{
		protocol.BlockUntilExplicitRestartOrRelevantInputChange,
		protocol.RetryAfterExtendedDelay,
		protocol.RetryAfterStandardDelay,
	} {
		if !containsForbiddenPublicFailureMaterial(string(fallback), forbidden) {
			return fallback
		}
	}
	return ""
}

func validPublicFailureRecommendation(value protocol.AuthenticationProtocolFailureHandlingRecommendation) bool {
	return value == protocol.RetryAfterStandardDelay ||
		value == protocol.RetryAfterExtendedDelay ||
		value == protocol.BlockUntilExplicitRestartOrRelevantInputChange
}

func (session *AuthenticationSession) resetRetryState() {
	session.consecutiveFailures = 0
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
	if !session.runtimeDefinitionAvailable || session.shuttingDown || session.active != nil || session.currentSnapshot.Intent != MaintainAuthentication || !session.hasDesiredBinding || session.automaticReconnectBlocked {
		return
	}
	if !allowRecoveryState && (session.currentSnapshot.State == BlockedByError || session.currentSnapshot.State == WaitingBeforeRetry) {
		return
	}
	session.nextGeneration++
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
	active := &activeProtocolRun{generation: generation, run: run, context: ctx, cancel: cancel}
	session.active = active
	session.diagnostics.ProtocolRunGeneration(generation)
	session.updateSnapshot(func(snapshot *Snapshot) {
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
	session.active.cancel(protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: requirement})
}

func (session *AuthenticationSession) updateSnapshot(mutate func(*Snapshot)) {
	before := session.currentSnapshot.Clone()
	mutate(&session.currentSnapshot)
	if publicSnapshotsEqual(before, session.currentSnapshot) {
		return
	}
	session.currentSnapshot.Revision++
	session.currentSnapshot.UpdatedAt = session.now()
	session.publishRevision()
}

func publicSnapshotsEqual(left, right Snapshot) bool {
	left.Revision, right.Revision = 0, 0
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(left, right)
}

func (session *AuthenticationSession) publishInitialRevision() { session.publishRevision() }

func (session *AuthenticationSession) publishRevision() {
	event := RevisionEvent{AuthenticationSessionID: session.currentSnapshot.AuthenticationSessionID, Revision: session.currentSnapshot.Revision}
	select {
	case session.revisions <- event:
	default:
		select {
		case <-session.revisions:
		default:
		}
		select {
		case session.revisions <- event:
		default:
		}
	}
	// The diagnostic sink observes every committed revision directly, without
	// going through the coalescing RevisionEvents stream, so no intermediate
	// public revision is lost. It has no return value and cannot change state.
	session.diagnostics.SessionSnapshot(session.currentSnapshot.Clone())
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
