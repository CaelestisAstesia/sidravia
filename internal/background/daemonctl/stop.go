package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"sidravia/internal/ipc/contract"
)

type StopStage string

const (
	StopConnect     StopStage = "connect"
	StopCall        StopStage = "call"
	StopRejected    StopStage = "rejected"
	StopUnconfirmed StopStage = "unconfirmed"
)

type StopFailure struct {
	Stage      StopStage
	Code       string
	Cause      error
	CloseCause error
}

func (failure *StopFailure) Error() string { return "daemon stop " + string(failure.Stage) }
func (failure *StopFailure) Unwrap() error { return errors.Join(failure.Cause, failure.CloseCause) }

// StopGeneration requests ordinary IPC shutdown against the exact generation
// already selected by the caller. It never reopens discovery or stops a PID.
func StopGeneration(ctx context.Context, identity Identity, info contract.RuntimeInfo) error {
	return stopGeneration(ctx, identity, info, defaultDependencies(), 10*time.Second)
}

func stopGeneration(ctx context.Context, identity Identity, info contract.RuntimeInfo, deps dependencies, maxWait time.Duration) error {
	callCtx, cancelCall := context.WithTimeout(ctx, deps.callTimeout)
	defer cancelCall()
	connection, err := connectWithDependencies(callCtx, identity, info, deps)
	if err != nil {
		return &StopFailure{Stage: StopConnect, Cause: err}
	}
	response, err := connection.Call(callCtx, contract.MethodDaemonStop, json.RawMessage("{}"))
	closeErr := connection.Close()
	if err != nil {
		return &StopFailure{Stage: StopCall, Cause: err, CloseCause: closeErr}
	}
	if !response.OK {
		var code string
		if response.Error != nil {
			code = response.Error.Code
		}
		return &StopFailure{Stage: StopRejected, Code: code}
	}
	var ack contract.DaemonStopResult
	if err := json.Unmarshal(response.Result, &ack); err != nil || ack.Status != "stopping" {
		return &StopFailure{Stage: StopUnconfirmed, Cause: err}
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, maxWait)
	defer cancelWait()
	ticker := time.NewTicker(deps.pollInterval)
	defer ticker.Stop()
	var observation error
	for {
		select {
		case <-waitCtx.Done():
			return &StopFailure{Stage: StopUnconfirmed, Cause: errors.Join(waitCtx.Err(), observation)}
		case <-ticker.C:
			gone, err := stoppedIPC(waitCtx, deps, identity, info)
			if gone {
				return nil
			}
			observation = err
		}
	}
}

func stoppedIPC(ctx context.Context, deps dependencies, identity Identity, info contract.RuntimeInfo) (bool, error) {
	pollCtx, cancel := context.WithTimeout(ctx, deps.callTimeout)
	defer cancel()
	connection, err := deps.connect(pollCtx, info, identity)
	if err != nil {
		// The loopback listener's explicit refusal confirms IPC is gone. A
		// timeout, authentication failure or malformed reply remains unknown.
		return connectionRefused(err), err
	}
	defer connection.Close()

	response, err := connection.Call(pollCtx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !response.OK {
		return false, err
	}
	var status contract.StatusResult
	if err := json.Unmarshal(response.Result, &status); err != nil {
		return false, err
	}
	return status.PID > 0 && status.PID != info.PID && status.ProductVersion == identity.ProductVersion && status.BuildID == identity.BuildID, nil
}
