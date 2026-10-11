package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
	"testing"
	"time"
)

func TestGUIRestartChecksOwnerBeforeStopAndAwaitBeforeStart(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "foreign"}[foreign], func(t *testing.T) {
			owner := 42
			actual := 42
			if foreign {
				actual = 43
			}
			identity := mustTestIdentity(t)
			stops, starts := 0, 0
			reachable := true
			deps := stopDependencies{totalWait: time.Second, pollInterval: time.Millisecond, ops: daemonOperations{
				inspect: func() (clientbootstrap.ProbeResult, error) {
					return clientbootstrap.ProbeResult{State: clientbootstrap.ProbeReachable, Info: testRuntimeInfo(7), Status: &contract.StatusResult{Mode: "desktop", DesktopOwnerPID: &actual}}, nil
				},
				connect: func(context.Context, contract.RuntimeInfo) (daemonClient, error) {
					return &fakeDaemonClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
						stops++
						reachable = false
						if method != contract.MethodDaemonStop {
							t.Fatal(method)
						}
						return contract.NewSuccessResponse("1", json.RawMessage(`{"status":"stopping"}`)), nil
					}}, nil
				},
				reachable: func(contract.RuntimeInfo) bool { return reachable },
			}}
			start := func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
				starts++
				if reachable {
					t.Fatal("started before stopped")
				}
				return clientbootstrap.DesktopBootstrapResult{Info: testRuntimeInfo(8), Status: contract.StatusResult{ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, PID: 8, Mode: "desktop", DesktopOwnerPID: &owner, LogLevel: "debug"}}, nil
			}
			var out bytes.Buffer
			err := runGUIRestart(owner, "debug", deps, start, identity, &out)
			if foreign {
				if err == nil || stops != 0 || starts != 0 {
					t.Fatalf("foreign owner touched: %v %d %d", err, stops, starts)
				}
			} else {
				if err != nil || stops != 1 || starts != 1 {
					t.Fatalf("restart=%v stop=%d start=%d", err, stops, starts)
				}
			}
		})
	}
}
func TestGUIRestartStopFailureNeverStartsReplacement(t *testing.T) {
	owner := 42
	started := false
	deps := stopDependencies{ops: daemonOperations{
		inspect: func() (clientbootstrap.ProbeResult, error) {
			return clientbootstrap.ProbeResult{State: clientbootstrap.ProbeReachable, Info: testRuntimeInfo(7), Status: &contract.StatusResult{Mode: "desktop", DesktopOwnerPID: &owner}}, nil
		},
		connect: func(context.Context, contract.RuntimeInfo) (daemonClient, error) {
			return nil, errors.New("stop unavailable")
		},
	}}
	var out bytes.Buffer
	err := runGUIRestart(owner, "info", deps, func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error) {
		started = true
		return clientbootstrap.DesktopBootstrapResult{}, nil
	}, mustTestIdentity(t), &out)
	if err == nil || started || out.Len() != 0 {
		t.Fatal("failed stop launched replacement")
	}
}
