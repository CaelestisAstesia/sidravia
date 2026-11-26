package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

func TestStopGenerationUsesOnlySelectedEndpointAndDistinguishesOutcome(t *testing.T) {
	failedCall := errors.New("transport interrupted")
	unknown := errors.New("observation unavailable")
	for _, tc := range []struct {
		name       string
		stop       func() (contract.Response, error)
		connectErr error
		observeErr error
		stage      StopStage
		cause      error
		code       string
	}{
		{name: "replacement observed", stop: acceptedStop},
		{name: "connect failed", connectErr: failedCall, stage: StopConnect, cause: failedCall},
		{name: "request failed", stop: func() (contract.Response, error) { return contract.Response{}, failedCall }, stage: StopCall, cause: failedCall},
		{name: "daemon refused", stop: func() (contract.Response, error) {
			return contract.Response{Error: &contract.Error{Code: contract.ErrorCodeMaintenance, Message: "private"}}, nil
		}, stage: StopRejected, code: contract.ErrorCodeMaintenance},
		{name: "unobservable IPC", stop: acceptedStop, observeErr: unknown, stage: StopUnconfirmed, cause: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := runtime(1)
			dials := 0
			identity := testIdentity(t)
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
				t.Fatal("stop re-read runtime information")
				return contract.RuntimeInfo{}, nil
			}, func(_ context.Context, target contract.RuntimeInfo, clientIdentity Identity) (Client, error) {
				if target != selected || clientIdentity != identity {
					t.Fatal("stop used a different endpoint or client identity")
				}
				dials++
				if dials == 1 {
					if tc.connectErr != nil {
						return nil, tc.connectErr
					}
					return &fakeClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
						if method != contract.MethodDaemonStop {
							t.Fatalf("first call used %q", method)
						}
						return tc.stop()
					}}, nil
				}
				if tc.observeErr != nil {
					return nil, tc.observeErr
				}
				return &fakeClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
					if method != contract.MethodDaemonStatus {
						t.Fatalf("poll used %q", method)
					}
					return statusResponseValue(t, contract.StatusResult{
						ProductVersion: identity.ProductVersion, BuildID: identity.BuildID,
						PID: 2, Mode: "headless", Status: "running",
					}), nil
				}}, nil
			})
			err := stopGeneration(context.Background(), identity, selected, deps, 20*time.Millisecond)
			if tc.stage == "" {
				if err != nil || dials != 2 {
					t.Fatalf("expected observed stop, dials=%d error=%v", dials, err)
				}
				return
			}
			var failure *StopFailure
			if !errors.As(err, &failure) || failure.Stage != tc.stage || failure.Code != tc.code || (tc.cause != nil && !errors.Is(err, tc.cause)) {
				t.Fatalf("stop failure = %#v (%v), expected %s", failure, err, tc.stage)
			}
			if tc.stage != StopUnconfirmed && dials != 1 {
				t.Fatalf("polled after failed stop: dials=%d", dials)
			}
		})
	}
}

func acceptedStop() (contract.Response, error) {
	return contract.NewSuccessResponse("1", json.RawMessage(`{"status":"stopping"}`)), nil
}
