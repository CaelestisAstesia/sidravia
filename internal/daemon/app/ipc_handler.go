package app

import (
	"context"
	"encoding/json"

	"sidravia/internal/ipc/contract"
)

// IPCHandler returns a transport-neutral handler that routes a fixed set of IPC
// methods to the existing status, Session and Profile handlers. It performs no prefix
// matching, reflection, arbitrary handler registration or generic RPC dispatch.
//
// daemon.status is routed to StatusHandler; session.startOneShot, session.stop,
// session.get and session.list are routed to SessionHandler; profile.list is
// routed to ProfileHandler. Every other method returns
// unknown_method with one static generic message. The handler never logs,
// persists or reproduces a request payload or password.
//
// This composes handlers in memory only. It adds no WebSocket dependency and
// does not modify internal/ipc/server or cmd/sidraviad.
func IPCHandler(
	application *Application,
	productVersion, buildID string,
) func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	status := StatusHandler(productVersion, buildID)
	sessions := SessionHandler(application)
	profiles := ProfileHandler(application)

	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		switch method {
		case contract.MethodDaemonStatus:
			return status(ctx, method, payload)
		case contract.MethodSessionStartOneShot,
			contract.MethodSessionStop,
			contract.MethodSessionGet,
			contract.MethodSessionList:
			return sessions(ctx, method, payload)
		case contract.MethodProfileList:
			return profiles(ctx, method, payload)
		default:
			return nil, &contract.Error{
				Code:    contract.ErrorCodeUnknownMethod,
				Message: "unsupported method",
			}
		}
	}
}
