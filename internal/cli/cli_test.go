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
	}
	for _, args := range cases {
		err := Run(args)
		if err == nil {
			t.Errorf("Run(%v) = nil, want error", args)
			continue
		}
		if got := err.Error(); got != "usage: sidravia status" {
			t.Errorf("Run(%v) = %q, want %q", args, got, "usage: sidravia status")
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
