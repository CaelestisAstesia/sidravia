package session

import (
	"context"
	"sync/atomic"

	"sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

type sessionMessage interface{ isSessionMessage() }

type snapshotReply struct {
	snapshot Snapshot
	err      error
}

type systemNetworkSnapshotCommand struct {
	network environment.Snapshot
	reply   chan snapshotReply
}

type activateCommand struct{ reply chan snapshotReply }
type suspendCommand struct{ reply chan snapshotReply }
type restartCommand struct{ reply chan snapshotReply }

type replaceRuntimeDefinitionCommand struct {
	replacement runtimeDefinitionReplacement
	reply       chan snapshotReply
}

type runtimeDefinitionReplacement struct {
	definition        *RuntimeDefinition
	unresolved        *unresolvedRuntimeDefinition
	preserveSessionID bool
}

func (replacement runtimeDefinitionReplacement) clone() runtimeDefinitionReplacement {
	cloned := runtimeDefinitionReplacement{}
	cloned.preserveSessionID = replacement.preserveSessionID
	if replacement.definition != nil {
		definition := replacement.definition.Clone()
		definition.Configuration.ProtocolContextOverride = cloneProtocolContextOverride(
			replacement.definition.Configuration.ProtocolContextOverride,
		)
		definition.InstitutionProfile.InstitutionProtocolConfiguration = cloneInstitutionProtocolConfiguration(
			replacement.definition.InstitutionProfile.InstitutionProtocolConfiguration,
		)
		cloned.definition = &definition
	}
	if replacement.unresolved != nil {
		unresolved := replacement.unresolved.Clone()
		cloned.unresolved = &unresolved
	}
	return cloned
}

func cloneInstitutionProtocolConfiguration(
	configuration protocol.InstitutionProtocolConfiguration,
) protocol.InstitutionProtocolConfiguration {
	if configuration == nil {
		return nil
	}
	cloned := make(protocol.InstitutionProtocolConfiguration, len(configuration))
	copy(cloned, configuration)
	return cloned
}

func (replacement runtimeDefinitionReplacement) valid() bool {
	return (replacement.definition == nil) != (replacement.unresolved == nil)
}

type shutdownCommand struct {
	reply       chan error
	preparation *shutdownPreparation
}

type shutdownPreparationDecision uint8

const (
	shutdownPreparationAbort shutdownPreparationDecision = iota + 1
	shutdownPreparationCommit
)

type shutdownPreparation struct {
	prepared    chan Snapshot
	decision    chan shutdownPreparationDecision
	decisionAck chan struct{}
	decided     atomic.Bool
}

func newShutdownPreparation() *shutdownPreparation {
	return &shutdownPreparation{
		prepared:    make(chan Snapshot, 1),
		decision:    make(chan shutdownPreparationDecision, 1),
		decisionAck: make(chan struct{}),
	}
}

func (preparation *shutdownPreparation) Abort() bool {
	return preparation.decide(shutdownPreparationAbort)
}

func (preparation *shutdownPreparation) Commit() bool {
	return preparation.decide(shutdownPreparationCommit)
}

func (preparation *shutdownPreparation) decide(decision shutdownPreparationDecision) bool {
	if preparation == nil || !preparation.decided.CompareAndSwap(false, true) {
		return false
	}
	preparation.decision <- decision
	return true
}

type snapshotQuery struct{ reply chan snapshotReply }

type authenticationEstablishedEvent struct{ generation uint64 }

type authenticationProtocolRunFinishedEvent struct {
	generation        uint64
	failure           *protocol.AuthenticationProtocolRunFailure
	cancellationCause error
}

type authenticationRetryDelayElapsedEvent struct{ scheduleID uint64 }
type suspensionCompletedEvent struct{}

func (systemNetworkSnapshotCommand) isSessionMessage()           {}
func (activateCommand) isSessionMessage()                        {}
func (suspendCommand) isSessionMessage()                         {}
func (restartCommand) isSessionMessage()                         {}
func (replaceRuntimeDefinitionCommand) isSessionMessage()        {}
func (shutdownCommand) isSessionMessage()                        {}
func (snapshotQuery) isSessionMessage()                          {}
func (authenticationEstablishedEvent) isSessionMessage()         {}
func (authenticationProtocolRunFinishedEvent) isSessionMessage() {}
func (authenticationRetryDelayElapsedEvent) isSessionMessage()   {}
func (suspensionCompletedEvent) isSessionMessage()               {}

func (session *AuthenticationSession) send(ctx context.Context, message sessionMessage) error {
	if err := session.acquireAdmission(ctx); err != nil {
		return err
	}
	defer session.releaseAdmission()
	select {
	case session.inbox <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
		return ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) sendFromWatcher(ctx context.Context, message sessionMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-session.admission:
		defer session.releaseAdmission()
		select {
		case session.inbox <- message:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-session.done:
			return ErrAuthenticationSessionClosed
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
		return ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) beginShutdown(ctx context.Context, command shutdownCommand) error {
	if err := session.acquireAdmission(ctx); err != nil {
		return err
	}
	defer session.releaseAdmission()

	session.closed.Store(true)
	select {
	case session.inbox <- command:
		return nil
	case <-ctx.Done():
		session.closed.Store(false)
		return ctx.Err()
	case <-session.done:
		return ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) acquireAdmission(ctx context.Context) error {
	if session.closed.Load() {
		return ErrAuthenticationSessionClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-session.admission:
		if session.closed.Load() {
			session.releaseAdmission()
			return ErrAuthenticationSessionClosed
		}
		if err := ctx.Err(); err != nil {
			session.releaseAdmission()
			return err
		}
		return nil
	case <-ctx.Done():
		if session.closed.Load() {
			return ErrAuthenticationSessionClosed
		}
		return ctx.Err()
	case <-session.done:
		return ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) releaseAdmission() {
	session.admission <- struct{}{}
}

func (session *AuthenticationSession) post(message sessionMessage) {
	select {
	case <-session.admission:
		defer session.releaseAdmission()
		select {
		case session.inbox <- message:
		case <-session.done:
		}
	case <-session.done:
	}
}
