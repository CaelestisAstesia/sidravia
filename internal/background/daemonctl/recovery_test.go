package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
)

func TestRecoveryNeverAdoptsAConcurrentGeneration(t *testing.T) {
	for _, mode := range []string{"own", "foreign", "own-exited", "observation-error", "hot"} {
		t.Run(mode, func(t *testing.T) {
			reads, launched, released := 0, 0, 0
			observationErr := errors.New("process observation unavailable")
			pid := 1
			if mode == "foreign" || mode == "hot" {
				pid = 2
			}
			deps := testDependencies(func(string) (contract.RuntimeInfo, error) {
				reads++
				if reads == 1 && mode != "hot" {
					return contract.RuntimeInfo{}, os.ErrNotExist
				}
				return runtime(pid), nil
			}, func(_ context.Context, info contract.RuntimeInfo, _ Identity) (Client, error) {
				return &fakeClient{call: func(string, json.RawMessage) (contract.Response, error) {
					return statusResponseValue(t, contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build", PID: info.PID, Status: "running", Mode: "headless"}), nil
				}}, nil
			})
			deps.launch = func(options launchcontract.Options, _ string) (daemonLaunch, error) {
				launched++
				if !options.SkipAutoLogin {
					t.Fatal("default auto-login not suppressed")
				}
				return daemonLaunch{pid: 1, alive: func() (bool, error) {
					if mode == "observation-error" {
						return false, observationErr
					}
					return mode != "own-exited", nil
				}, release: func() { released++ }}, nil
			}
			info, err := startHeadlessForRecovery(testIdentity(t), "debug", deps)
			if mode == "own" {
				if err != nil || info.PID != 1 {
					t.Fatalf("%+v %v", info, err)
				}
			} else if mode == "observation-error" {
				if !errors.Is(err, observationErr) || errors.Is(err, ErrModeConflict) || info.PID != 0 {
					t.Fatalf("observation error was not preserved: %+v %v", info, err)
				}
			} else if !errors.Is(err, ErrModeConflict) {
				t.Fatalf("adopted %s: %+v %v", mode, info, err)
			}
			if mode == "hot" {
				if launched != 0 {
					t.Fatal("launched over hot process")
				}
			} else if released != 1 {
				t.Fatal("process observation leaked")
			}
		})
	}
}
