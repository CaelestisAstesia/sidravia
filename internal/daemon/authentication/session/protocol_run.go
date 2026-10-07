package session

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"sidravia/internal/daemon/authentication/protocol"
)

type protocolRunObserver struct {
	session    *AuthenticationSession
	generation uint64
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
		failure = run.run.Execute(run.context, protocolRunObserver{session: session, generation: run.generation})
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
