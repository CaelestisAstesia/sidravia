package app

import (
	"context"
	"encoding/json"

	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

// IPCHandler returns a transport-neutral handler that routes a fixed set of IPC
// methods to the existing status, daemon, Session and Profile handlers. It
// performs no prefix matching, reflection, arbitrary handler registration or
// generic RPC dispatch.
//
// daemon.status is routed to StatusHandler; daemon.stop is routed to
// DaemonHandler; session.startOneShot, session.stop, session.get and
// session.list are routed to SessionHandler; profile.list is routed to
// ProfileHandler. Every other method returns unknown_method with one static
// generic message. The handler never logs, persists or reproduces a request
// payload or password.
func IPCHandler(
	application *Application,
	productVersion, buildID string,
	options launchcontract.Options,
) func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	status := StatusHandler(productVersion, buildID, options)
	daemon := DaemonHandler()
	sessions := SessionHandler(application)
	profiles := ProfileHandler(application)
	configurations := ConfigurationHandler(application)
	network := NetworkHandler(application)

	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		switch method {
		case contract.MethodDaemonStatus:
			return status(ctx, method, payload)
		case contract.MethodDaemonStop:
			return daemon(ctx, method, payload)
		case contract.MethodSessionStartOneShot,
			contract.MethodSessionStop,
			contract.MethodSessionEnsureRunning,
			contract.MethodSessionRestart,
			contract.MethodSessionRemove,
			contract.MethodSessionGet,
			contract.MethodSessionList:
			return sessions(ctx, method, payload)
		case contract.MethodSessionStartConfiguration:
			return sessions(ctx, method, payload)
		case contract.MethodConfigurationList,
			contract.MethodConfigurationGet,
			contract.MethodConfigurationCreate,
			contract.MethodConfigurationUpdate,
			contract.MethodConfigurationSetPassword,
			contract.MethodConfigurationRemove:
			return configurations(ctx, method, payload)
		case contract.MethodNetworkInterfaces:
			return network(ctx, method, payload)
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
