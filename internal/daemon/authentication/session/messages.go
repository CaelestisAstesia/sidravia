package session

import (
	"context"
	"net/netip"

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

type shutdownCommand struct {
	reply chan error
}

type snapshotQuery struct{ reply chan snapshotReply }

type diagnosticSnapshotReply struct{ snapshot DiagnosticSnapshot }
type diagnosticSnapshotQuery struct{ reply chan diagnosticSnapshotReply }
type protocolSocketOpenedEvent struct {
	generation    uint64
	local, remote netip.AddrPort
}
type protocolSocketClosedEvent struct {
	generation uint64
	closed     bool
}

func (diagnosticSnapshotQuery) isSessionMessage()   {}
func (protocolSocketOpenedEvent) isSessionMessage() {}
func (protocolSocketClosedEvent) isSessionMessage() {}

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
