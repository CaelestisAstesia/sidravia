package app

import (
	"context"
	"encoding/json"
	"os"

	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func StatusHandler(productVersion string, buildID string, options launchcontract.Options) func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	pid := os.Getpid()
	ownerPID := (*int)(nil)
	if options.Mode == launchcontract.ModeDesktop {
		owner := options.DesktopOwnerPID
		ownerPID = &owner
	}

	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDaemonStatus {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeUnknownMethod,
				Message: "unsupported method",
			}
		}
		// daemon.status is strictly read-only and accepts only the canonical
		// empty JSON object. null, unknown fields and trailing data are rejected.
		if err := contract.DecodeEmptyPayload(payload); err != nil {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeInvalidArgument,
				Message: "malformed daemon payload",
			}
		}

		result, err := contract.MarshalStatusResult(contract.StatusResult{
			ProductVersion:  productVersion,
			BuildID:         buildID,
			PID:             pid,
			Status:          "running",
			Mode:            string(options.Mode),
			DesktopOwnerPID: ownerPID,
		})
		if err != nil {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeMalformed,
				Message: "failed to marshal status result",
			}
		}
		return result, nil
	}
}
