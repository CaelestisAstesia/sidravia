package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

// testRuntimeInfo returns a RuntimeInfo that would pass contract.DecodeRuntimeInfo,
// so behavior tests can treat it as a legitimate daemon bootstrap.
func testRuntimeInfo(pid int) contract.RuntimeInfo {
	return contract.RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:65530/ipc",
		PID:            pid,
		Token:          strings.Repeat("a", 64),
		ProductVersion: "1.0.0",
		BuildID:        "test-build",
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	cases := [][]string{
		nil,
		{"bogus"},
		{"status", "extra"},
		{"login"},
		{"logout"},
		{"auth"},
		{"auth", "watch", "session-marker"},
		{"auth", "check", "session-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--password", "value-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--wait"},
		{"auth", "status", "session-marker", "--json"},
	}
	for _, args := range cases {
		err := Run(args)
		if err == nil {
			t.Error("Run returned nil, want usage error")
			continue
		}
		if got := err.Error(); got != commandUsage {
			t.Errorf("Run error = %q, want static usage", got)
		}
		for _, marker := range []string{"profile-marker", "user-marker", "session-marker", "value-marker"} {
			if strings.Contains(err.Error(), marker) {
				t.Errorf("usage error contains supplied marker %q", marker)
			}
		}
	}
}

func TestRunDispatchesAcceptedCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "daemon status", args: []string{"status"}, want: "status"},
		{name: "interactive start", args: []string{"auth", "start", "--profile", "profile-a", "--username", "account-a"}, want: "start:profile-a:account-a:false"},
		{name: "stdin start", args: []string{"auth", "start", "--username", "account-b", "--password-stdin", "--profile", "profile-b"}, want: "start:profile-b:account-b:true"},
		{name: "session status", args: []string{"auth", "status", "session-a"}, want: "auth-status:session-a"},
		{name: "session stop", args: []string{"auth", "stop", "session-b"}, want: "auth-stop:session-b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got string
			deps := commandDependencies{
				status: func() error {
					got = "status"
					return nil
				},
				authStart: func(options authStartOptions) error {
					got = fmt.Sprintf("start:%s:%s:%t", options.profileID, options.username, options.passwordStdin)
					return nil
				},
				authStatus: func(sessionID string) error {
					got = "auth-status:" + sessionID
					return nil
				},
				authStop: func(sessionID string) error {
					got = "auth-stop:" + sessionID
					return nil
				},
			}
			if err := runCommand(test.args, deps); err != nil {
				t.Fatalf("runCommand = %v, want nil", err)
			}
			if got != test.want {
				t.Errorf("dispatch = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAuthStartRejectsInvalidArgumentsSafely(t *testing.T) {
	tests := [][]string{
		{"auth", "start"},
		{"auth", "start", "--profile", "profile-marker"},
		{"auth", "start", "--username", "user-marker"},
		{"auth", "start", "--profile", "", "--username", "user-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username", ""},
		{"auth", "start", "--profile", "profile-marker", "--profile", "other-marker", "--username", "user-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--username", "other-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--password-stdin", "--password-stdin"},
		{"auth", "start", "--profile=profile-marker", "--username", "user-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username=user-marker"},
		{"auth", "start", "-p", "profile-marker", "--username", "user-marker"},
		{"auth", "start", "--profile", "profile-marker", "-u", "user-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--unknown"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "positional-marker"},
		{"auth", "start", "--profile", "--username", "user-marker"},
		{"auth", "start", "--profile", "profile-marker", "--username"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--password"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--wait"},
		{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--json"},
	}

	deps := commandDependencies{
		status:     func() error { return nil },
		authStart:  func(authStartOptions) error { t.Fatal("invalid start dispatched"); return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}
	for _, args := range tests {
		err := runCommand(args, deps)
		if err == nil {
			t.Error("invalid auth start returned nil")
			continue
		}
		if err.Error() != commandUsage {
			t.Errorf("invalid auth start error = %q, want static usage", err)
		}
		for _, marker := range []string{"profile-marker", "user-marker", "other-marker", "positional-marker"} {
			if strings.Contains(err.Error(), marker) {
				t.Errorf("usage error contains supplied marker %q", marker)
			}
		}
	}
}

func TestAuthStatusAndStopRejectInvalidSessionArguments(t *testing.T) {
	tests := [][]string{
		{"auth", "status"},
		{"auth", "status", ""},
		{"auth", "status", "session-marker", "extra-marker"},
		{"auth", "status", "--session-marker"},
		{"auth", "stop"},
		{"auth", "stop", ""},
		{"auth", "stop", "session-marker", "extra-marker"},
		{"auth", "stop", "--session-marker"},
	}
	deps := commandDependencies{
		status:     func() error { return nil },
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { t.Fatal("invalid status dispatched"); return nil },
		authStop:   func(string) error { t.Fatal("invalid stop dispatched"); return nil },
	}
	for _, args := range tests {
		err := runCommand(args, deps)
		if err == nil {
			t.Error("invalid session command returned nil")
			continue
		}
		if err.Error() != commandUsage {
			t.Errorf("invalid session command error = %q, want static usage", err)
		}
		if strings.Contains(err.Error(), "marker") {
			t.Error("usage error contains supplied session marker")
		}
	}
}

func TestStatusHotConnect(t *testing.T) {
	info := testRuntimeInfo(100)
	reads, connects, starts := 0, 0, 0
	var connected contract.RuntimeInfo

	deps := statusDependencies{
		runtimeInfoPath: func() (string, error) { return "hot-path", nil },
		readRuntimeInfo: func(path string) (contract.RuntimeInfo, error) {
			reads++
			if path != "hot-path" {
				t.Errorf("readRuntimeInfo path = %q, want %q", path, "hot-path")
			}
			return info, nil
		},
		connectAndPrint: func(got contract.RuntimeInfo) error {
			connects++
			connected = got
			return nil
		},
		startDaemon: func() error {
			starts++
			return nil
		},
		totalWait:    5 * time.Second,
		pollInterval: 10 * time.Millisecond,
	}

	if err := runStatus(deps); err != nil {
		t.Fatalf("runStatus = %v, want nil", err)
	}
	if reads != 1 {
		t.Errorf("readRuntimeInfo calls = %d, want 1", reads)
	}
	if connects != 1 {
		t.Errorf("connectAndPrint calls = %d, want 1", connects)
	}
	if starts != 0 {
		t.Errorf("startDaemon calls = %d, want 0", starts)
	}
	if connected != info {
		t.Errorf("connectAndPrint received %+v, want %+v", connected, info)
	}
}

func TestStatusColdStart(t *testing.T) {
	info := testRuntimeInfo(200)
	reads, connects, starts := 0, 0, 0
	var connected contract.RuntimeInfo

	deps := statusDependencies{
		runtimeInfoPath: func() (string, error) { return "cold-path", nil },
		readRuntimeInfo: func(path string) (contract.RuntimeInfo, error) {
			reads++
			if reads == 1 {
				return contract.RuntimeInfo{}, errors.New("runtime info not found")
			}
			return info, nil
		},
		connectAndPrint: func(got contract.RuntimeInfo) error {
			connects++
			connected = got
			return nil
		},
		startDaemon: func() error {
			starts++
			return nil
		},
		totalWait:    5 * time.Second,
		pollInterval: 5 * time.Millisecond,
	}

	if err := runStatus(deps); err != nil {
		t.Fatalf("runStatus = %v, want nil", err)
	}
	if starts != 1 {
		t.Errorf("startDaemon calls = %d, want 1", starts)
	}
	if reads != 2 {
		t.Errorf("readRuntimeInfo calls = %d, want 2", reads)
	}
	if connects != 1 {
		t.Errorf("connectAndPrint calls = %d, want 1", connects)
	}
	if connected != info {
		t.Errorf("connectAndPrint received %+v, want %+v", connected, info)
	}
}

func TestStatusStaleInfoRecovery(t *testing.T) {
	oldInfo := testRuntimeInfo(300)
	newInfo := testRuntimeInfo(301)
	reads, starts := 0, 0
	var connectInfos []contract.RuntimeInfo

	deps := statusDependencies{
		runtimeInfoPath: func() (string, error) { return "stale-path", nil },
		readRuntimeInfo: func(path string) (contract.RuntimeInfo, error) {
			reads++
			if reads == 1 {
				return oldInfo, nil
			}
			return newInfo, nil
		},
		connectAndPrint: func(got contract.RuntimeInfo) error {
			connectInfos = append(connectInfos, got)
			if got.PID == oldInfo.PID {
				return errors.New("connect failed: stale runtime info")
			}
			return nil
		},
		startDaemon: func() error {
			starts++
			return nil
		},
		totalWait:    5 * time.Second,
		pollInterval: 5 * time.Millisecond,
	}

	if err := runStatus(deps); err != nil {
		t.Fatalf("runStatus = %v, want nil", err)
	}
	if starts != 1 {
		t.Errorf("startDaemon calls = %d, want 1", starts)
	}
	if len(connectInfos) != 2 {
		t.Fatalf("connectAndPrint calls = %d, want 2", len(connectInfos))
	}
	if connectInfos[0] != oldInfo {
		t.Errorf("first connect received %+v, want old %+v", connectInfos[0], oldInfo)
	}
	if connectInfos[1] != newInfo {
		t.Errorf("second connect received %+v, want new %+v", connectInfos[1], newInfo)
	}
}

func TestProductionStatusDependencies(t *testing.T) {
	deps := defaultStatusDependencies()
	if deps.totalWait != 5*time.Second {
		t.Errorf("production totalWait = %v, want 5s", deps.totalWait)
	}
	if deps.pollInterval != 200*time.Millisecond {
		t.Errorf("production pollInterval = %v, want 200ms", deps.pollInterval)
	}
	if deps.runtimeInfoPath == nil || deps.readRuntimeInfo == nil ||
		deps.connectAndPrint == nil || deps.startDaemon == nil {
		t.Error("production statusDependencies has a nil collaborator")
	}
}

func TestStatusTimeout(t *testing.T) {
	starts := 0
	deps := statusDependencies{
		runtimeInfoPath: func() (string, error) { return "timeout-path", nil },
		readRuntimeInfo: func(path string) (contract.RuntimeInfo, error) {
			return contract.RuntimeInfo{}, errors.New("runtime info not found")
		},
		connectAndPrint: func(got contract.RuntimeInfo) error {
			return errors.New("connect failed")
		},
		startDaemon: func() error {
			starts++
			return nil
		},
		totalWait:    50 * time.Millisecond,
		pollInterval: 10 * time.Millisecond,
	}

	err := runStatus(deps)
	if err == nil {
		t.Fatal("runStatus = nil, want timeout error")
	}
	if got := err.Error(); got != "timed out waiting for sidraviad" {
		t.Errorf("runStatus = %q, want %q", got, "timed out waiting for sidraviad")
	}
	if starts != 1 {
		t.Errorf("startDaemon calls = %d, want 1", starts)
	}
}

func TestWriteStatusSuccessAuthoritative(t *testing.T) {
	result := contract.StatusResult{
		ProductVersion: "1.4.2",
		BuildID:        "build-deadbeef",
		PID:            9876,
		Status:         "running",
	}
	payload, err := contract.MarshalStatusResult(result)
	if err != nil {
		t.Fatalf("marshal status result: %v", err)
	}
	resp := contract.NewSuccessResponse("1", payload)

	var buf bytes.Buffer
	if err := writeStatus(&buf, resp); err != nil {
		t.Fatalf("writeStatus = %v, want nil", err)
	}

	// The expected line is built only from the Response.
	var authoritative contract.StatusResult
	if err := json.Unmarshal(resp.Result, &authoritative); err != nil {
		t.Fatalf("unmarshal response result: %v", err)
	}
	expected := fmt.Sprintf("sidraviad %s (%s) pid=%d status=%s\n",
		authoritative.ProductVersion, authoritative.BuildID, authoritative.PID, authoritative.Status)

	if got := buf.String(); got != expected {
		t.Errorf("writeStatus output = %q, want %q", got, expected)
	}
}

func TestWriteStatusDaemonError(t *testing.T) {
	resp := contract.NewErrorResponse("1", "internal", "daemon exploded")
	var buf bytes.Buffer
	err := writeStatus(&buf, resp)
	if err == nil {
		t.Fatal("writeStatus = nil, want error")
	}
	if buf.Len() != 0 {
		t.Errorf("writer = %q, want empty", buf.String())
	}
}

func TestWriteStatusMalformedResult(t *testing.T) {
	resp := contract.NewSuccessResponse("1", json.RawMessage("not-valid-json"))
	var buf bytes.Buffer
	err := writeStatus(&buf, resp)
	if err == nil {
		t.Fatal("writeStatus = nil, want error")
	}
	if buf.Len() != 0 {
		t.Errorf("writer = %q, want empty", buf.String())
	}
}
