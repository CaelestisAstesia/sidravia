package app

import (
	"context"
	"encoding/json"
	"os"

	"sidravia/internal/ipc/contract"
)

func StatusHandler(productVersion string, buildID string) func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	pid := os.Getpid()

	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDaemonStatus {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeUnknownMethod,
				Message: "unsupported method",
			}
		}

		result, err := contract.MarshalStatusResult(contract.StatusResult{
			ProductVersion: productVersion,
			BuildID:        buildID,
			PID:            pid,
			Status:         "running",
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
