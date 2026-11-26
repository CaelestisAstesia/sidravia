package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/ipc/contract"
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
			deps := daemonOperations{
				inspect: func() (daemonctl.ProbeResult, error) {
					return daemonctl.ProbeResult{State: daemonctl.ProbeReachable, Info: testRuntimeInfo(7), Status: &contract.StatusResult{Mode: "desktop", DesktopOwnerPID: &actual}}, nil
				},
				stop: func(_ context.Context, info contract.RuntimeInfo) error {
					if info.PID != 7 {
						t.Fatalf("stop targeted PID %d", info.PID)
					}
					stops++
					reachable = false
					return nil
				},
			}
			start := func(daemonctl.Identity, int) (daemonctl.DesktopBootstrapResult, error) {
				starts++
				if reachable {
					t.Fatal("started before stopped")
				}
				return daemonctl.DesktopBootstrapResult{Info: testRuntimeInfo(8), Status: contract.StatusResult{ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, PID: 8, Mode: "desktop", DesktopOwnerPID: &owner, LogLevel: "debug"}}, nil
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
	deps := daemonOperations{
		inspect: func() (daemonctl.ProbeResult, error) {
			return daemonctl.ProbeResult{State: daemonctl.ProbeReachable, Info: testRuntimeInfo(7), Status: &contract.StatusResult{Mode: "desktop", DesktopOwnerPID: &owner}}, nil
		},
		stop: func(context.Context, contract.RuntimeInfo) error {
			return errors.New("stop unavailable")
		},
	}
	var out bytes.Buffer
	err := runGUIRestart(owner, "info", deps, func(daemonctl.Identity, int) (daemonctl.DesktopBootstrapResult, error) {
		started = true
		return daemonctl.DesktopBootstrapResult{}, nil
	}, mustTestIdentity(t), &out)
	if err == nil || started || out.Len() != 0 {
		t.Fatal("failed stop launched replacement")
	}
}
