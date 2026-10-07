package session

import (
	"context"
	"errors"
	"net/netip"
	"sidravia/internal/daemon/authentication/protocol"
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

// NetworkDiagnosticTarget is immutable trusted Profile and policy input. It
// contains neither credentials nor reported protocol-context overrides.
type NetworkDiagnosticTarget struct {
	Endpoint  netip.AddrPort
	Policy    NetworkBindingPolicy
	Supported bool
}

func (session *AuthenticationSession) NetworkDiagnosticTarget(ctx context.Context) (NetworkDiagnosticTarget, error) {
	if ctx == nil {
		return NetworkDiagnosticTarget{}, errors.New("network diagnostics context is required")
	}
	if ctx.Err() != nil {
		return NetworkDiagnosticTarget{}, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	if session.closed.Load() {
		return NetworkDiagnosticTarget{}, ErrAuthenticationSessionClosed
	}
	target := NetworkDiagnosticTarget{Policy: session.definition.Configuration.NetworkBindingPolicy}
	provider, ok := session.definition.AuthenticationProtocolFactory.(protocol.AuthenticationProtocolNetworkTargetProvider)
	if !ok {
		return target, nil
	}
	endpoint, err := provider.NetworkDiagnosticEndpoint(append(protocol.InstitutionProtocolConfiguration(nil), session.definition.InstitutionProfile.InstitutionProtocolConfiguration...))
	if err != nil {
		return NetworkDiagnosticTarget{}, err
	}
	if ctx.Err() != nil {
		return NetworkDiagnosticTarget{}, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	if session.closed.Load() {
		return NetworkDiagnosticTarget{}, ErrAuthenticationSessionClosed
	}
	target.Endpoint, target.Supported = endpoint, true
	return target, nil
}
