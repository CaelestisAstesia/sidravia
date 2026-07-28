package app

import (
	"context"
	"encoding/json"

	"sidravia/internal/ipc/contract"
)

// DaemonHandler returns an IPC handler for daemon lifecycle methods. It only
// acknowledges a stop request with the typed result {"status":"stopping"}; the
// actual runtime cancellation happens after the success response is written,
// via the server's responseCommitted hook mapped by the composition root. The
// handler never cancels runtime while building the response, so a failed
// encode/write does not stop the daemon.
func DaemonHandler() func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		switch method {
		case contract.MethodDaemonStop:
			return handleDaemonStop(payload)
		default:
			return nil, &contract.Error{
				Code:    contract.ErrorCodeUnknownMethod,
				Message: "unsupported daemon method",
			}
		}
	}
}

func handleDaemonStop(payload json.RawMessage) (json.RawMessage, *contract.Error) {
	if err := contract.DecodeEmptyPayload(payload); err != nil {
		return nil, &contract.Error{
			Code:    contract.ErrorCodeInvalidArgument,
			Message: "malformed daemon payload",
		}
	}
	result, err := contract.MarshalDaemonStopResult(contract.DaemonStopResult{Status: "stopping"})
	if err != nil {
		return nil, &contract.Error{
			Code:    contract.ErrorCodeMalformed,
			Message: "failed to encode daemon stop result",
		}
	}
	return result, nil
}
