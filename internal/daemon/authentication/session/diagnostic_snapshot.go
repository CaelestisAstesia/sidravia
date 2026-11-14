package session

import (
	"context"
	"errors"
	"net/netip"

	"time"
)

// ProtocolSocketState distinguishes observed closure from an unconfirmed exit.
type ProtocolSocketState string

const (
	ProtocolSocketNotObserved      ProtocolSocketState = "not_observed"
	ProtocolSocketOpen             ProtocolSocketState = "open"
	ProtocolSocketClosed           ProtocolSocketState = "closed"
	ProtocolSocketCloseFailed      ProtocolSocketState = "close_failed"
	ProtocolSocketCloseUnconfirmed ProtocolSocketState = "close_unconfirmed"
)

// ProtocolSocketObservation is an immutable value owned by the Session actor.
// Endpoints describe the observed Run generation, including after it exits.
type ProtocolSocketObservation struct {
	RunGeneration  uint64
	State          ProtocolSocketState
	LocalEndpoint  netip.AddrPort
	RemoteEndpoint netip.AddrPort
	UpdatedAt      time.Time
}

// DiagnosticSnapshot reads current desired binding and observed socket
// in one actor turn; a retained socket may belong to an earlier Run generation.
type DiagnosticSnapshot struct {
	Snapshot       Snapshot
	ProtocolSocket ProtocolSocketObservation
}

func (session *AuthenticationSession) QueryDiagnosticSnapshot(ctx context.Context) (DiagnosticSnapshot, error) {
	if ctx == nil {
		return DiagnosticSnapshot{}, errors.New("diagnostic snapshot context is required")
	}
	if ctx.Err() != nil {
		return DiagnosticSnapshot{}, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	reply := make(chan diagnosticSnapshotReply, 1)
	if err := session.send(ctx, diagnosticSnapshotQuery{reply: reply}); err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err(), context.Cause(ctx))
		}
		return DiagnosticSnapshot{}, err
	}
	select {
	case result := <-reply:
		return result.snapshot, nil
	case <-ctx.Done():
		return DiagnosticSnapshot{}, errors.Join(ctx.Err(), context.Cause(ctx))
	case <-session.done:
		return DiagnosticSnapshot{}, ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) handleProtocolSocketOpened(event protocolSocketOpenedEvent) {
	if session.active == nil || session.active.generation != event.generation || session.currentSnapshot.ProtocolSocket.RunGeneration != event.generation || session.currentSnapshot.ProtocolSocket.State != ProtocolSocketNotObserved {
		return
	}
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.ProtocolSocket = ProtocolSocketObservation{RunGeneration: event.generation, State: ProtocolSocketOpen, LocalEndpoint: event.local, RemoteEndpoint: event.remote, UpdatedAt: session.now()}
	})
}

func (session *AuthenticationSession) handleProtocolSocketClosed(event protocolSocketClosedEvent) {
	if session.active == nil || session.active.generation != event.generation || session.currentSnapshot.ProtocolSocket.RunGeneration != event.generation || session.currentSnapshot.ProtocolSocket.State != ProtocolSocketOpen {
		return
	}
	session.updateSnapshot(func(snapshot *Snapshot) {
		snapshot.ProtocolSocket.State = ProtocolSocketCloseFailed
		if event.closed {
			snapshot.ProtocolSocket.State = ProtocolSocketClosed
		}
		snapshot.ProtocolSocket.UpdatedAt = session.now()
	})
}
