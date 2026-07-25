package session

import (
	"context"
	"fmt"

	"sidravia/internal/daemon/authentication/protocol"
)

type protocolRunObserver struct {
	session    *AuthenticationSession
	generation uint64
}

func (observer protocolRunObserver) AuthenticationEstablished() {
	observer.session.post(authenticationEstablishedEvent{generation: observer.generation})
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
