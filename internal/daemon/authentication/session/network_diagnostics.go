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

// NetworkDiagnosticsSnapshot reads current desired binding and observed socket
// in one actor turn; a retained socket may belong to an earlier Run generation.
type NetworkDiagnosticsSnapshot struct {
	Snapshot       Snapshot
	ProtocolSocket ProtocolSocketObservation
}

func (session *AuthenticationSession) QueryNetworkDiagnostics(ctx context.Context) (NetworkDiagnosticsSnapshot, error) {
	if ctx == nil {
		return NetworkDiagnosticsSnapshot{}, errors.New("network diagnostics context is required")
	}
	if ctx.Err() != nil {
		return NetworkDiagnosticsSnapshot{}, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	reply := make(chan networkDiagnosticsReply, 1)
	if err := session.send(ctx, networkDiagnosticsQuery{reply: reply}); err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err(), context.Cause(ctx))
		}
		return NetworkDiagnosticsSnapshot{}, err
	}
	select {
	case result := <-reply:
		return result.snapshot, nil
	case <-ctx.Done():
		return NetworkDiagnosticsSnapshot{}, errors.Join(ctx.Err(), context.Cause(ctx))
	case <-session.done:
		return NetworkDiagnosticsSnapshot{}, ErrAuthenticationSessionClosed
	}
}

func (session *AuthenticationSession) handleProtocolSocketOpened(event protocolSocketOpenedEvent) {
	if session.active == nil || session.active.generation != event.generation || session.protocolSocket.RunGeneration != event.generation || session.protocolSocket.State != ProtocolSocketNotObserved {
		return
	}
	session.protocolSocket = ProtocolSocketObservation{RunGeneration: event.generation, State: ProtocolSocketOpen, LocalEndpoint: event.local, RemoteEndpoint: event.remote, UpdatedAt: session.now()}
}

func (session *AuthenticationSession) handleProtocolSocketClosed(event protocolSocketClosedEvent) {
	if session.active == nil || session.active.generation != event.generation || session.protocolSocket.RunGeneration != event.generation || session.protocolSocket.State != ProtocolSocketOpen {
		return
	}
	session.protocolSocket.State = ProtocolSocketCloseFailed
	if event.closed {
		session.protocolSocket.State = ProtocolSocketClosed
	}
	session.protocolSocket.UpdatedAt = session.now()
}
