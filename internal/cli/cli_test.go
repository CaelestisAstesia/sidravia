package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

func TestParseAuthStartRetainedMode(t *testing.T) {
	got, err := parseAuthStart([]string{"--session", "session-1"})
	if err != nil || got.sessionID != "session-1" {
		t.Fatalf("parse retained = %#v, %v", got, err)
	}
	if _, err := parseAuthStart([]string{"--session", "session-1", "--profile", "jlu"}); err == nil {
		t.Fatal("mixed retained and one-shot mode accepted")
	}
}

func TestParseAuthStartRetainedRejectsMissingMixedAndDuplicateForms(t *testing.T) {
	for _, args := range [][]string{
		{"--session"},
		{"--session", ""},
		{"--session", "session-1", "--session", "session-2"},
		{"--session", "session-1", "--username", "user"},
		{"--session", "session-1", "--profile", "profile"},
		{"--session", "session-1", "--password-stdin"},
		{"--profile", "profile"},
		{"--username", "user"},
		{"--profile", "profile", "--profile", "other", "--username", "user"},
		{"--profile", "profile", "--username", "user", "--username", "other"},
		{"--profile", "profile", "--username", "user", "--password-stdin", "--password-stdin"},
		{"--session", "session-1", "--profile", "profile", "--username", "user"},
		{"--session", "session-1", "--profile", "profile", "--username", "user", "--password-stdin"},
		{"--session", "session-1", "--unknown"},
		{"--profile", "profile", "--username", "user", "--session", "session-1"},
		{"--profile", "", "--username", "user"},
		{"--profile", "profile", "--username", ""},
		{"--profile"},
		{"--username"},
		{"--unknown"},
	} {
		if _, err := parseAuthStart(args); err == nil {
			t.Fatalf("accepted invalid retained start args: %q", args)
		}
	}
}

func TestRetainedCommandsDispatchAndHelpDoesNotDispatch(t *testing.T) {
	var starts []authStartOptions
	var restarts, removes []string
	deps := commandDependencies{
		authStart:   func(options authStartOptions) error { starts = append(starts, options); return nil },
		authRestart: func(id string) error { restarts = append(restarts, id); return nil },
		authRemove:  func(id string) error { removes = append(removes, id); return nil },
		output:      io.Discard,
	}
	for _, args := range [][]string{
		{"auth", "start", "--session", "session-1"},
		{"auth", "restart", "session-1"},
		{"auth", "remove", "session-1"},
	} {
		if err := runCommand(args, deps); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
	}
	if len(starts) != 1 || starts[0].sessionID != "session-1" || len(restarts) != 1 || restarts[0] != "session-1" || len(removes) != 1 || removes[0] != "session-1" {
		t.Fatalf("dispatch starts=%#v restarts=%v removes=%v", starts, restarts, removes)
	}
	for _, help := range []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"auth"}},
		{[]string{"auth", "--help"}, []string{"start", "restart", "remove"}},
		{[]string{"auth", "start", "--help"}, []string{
			"sidravia auth start --profile <profile-id> --username <username> [--password-stdin]",
			"sidravia auth start --session <session-id>",
		}},
		{[]string{"auth", "restart", "--help"}, []string{"sidravia auth restart <session-id>"}},
		{[]string{"auth", "remove", "--help"}, []string{"sidravia auth remove <session-id>"}},
	} {
		var output bytes.Buffer
		deps.output = &output
		if err := runCommand(help.args, deps); err != nil {
			t.Fatalf("help %q: %v", help.args, err)
		}
		for _, want := range help.want {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("help %q omitted %q:\n%s", help.args, want, output.String())
			}
		}
	}
	if len(starts) != 1 || len(restarts) != 1 || len(removes) != 1 {
		t.Fatal("help dispatched an operation")
	}
}

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
		{"daemon"},
		{"daemon", "status", "extra"},
		{"daemon", "start", "--log-level", "verbose"},
		{"daemon", "start", "extra"},
		{"daemon", "stop", "extra"},
		{"login"},
		{"logout"},
		{"auth"},
		{"list"},
		{"profile"},
		{"profile", "list", "extra"},
		{"profile", "list", "--json"},
		{"account", "list"},
		{"auth", "config", "list"},
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
		{name: "daemon status", args: []string{"daemon", "status"}, want: "status"},
		{name: "interactive start", args: []string{"auth", "start", "--profile", "profile-a", "--username", "account-a"}, want: "start:profile-a:account-a:false"},
		{name: "stdin start", args: []string{"auth", "start", "--username", "account-b", "--password-stdin", "--profile", "profile-b"}, want: "start:profile-b:account-b:true"},
		{name: "session status", args: []string{"auth", "status", "session-a"}, want: "auth-status:session-a"},
		{name: "session stop", args: []string{"auth", "stop", "session-b"}, want: "auth-stop:session-b"},
		{name: "session list", args: []string{"auth", "list"}, want: "auth-list"},
		{name: "profile list", args: []string{"profile", "list"}, want: "profile-list"},
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
				authList: func() error {
					got = "auth-list"
					return nil
				},
				profileList: func() error {
					got = "profile-list"
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

func TestRetiredStatusReturnsMigrationWithoutDispatch(t *testing.T) {
	dispatched := false
	deps := commandDependencies{
		status: func() error {
			dispatched = true
			return nil
		},
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}

	err := runCommand([]string{"status"}, deps)
	if err == nil {
		t.Fatal("retired status returned nil")
	}
	if err.Error() != "命令已迁移，请使用 sidravia daemon status" {
		t.Errorf("retired status error = %q", err)
	}
	if dispatched {
		t.Error("retired status dispatched daemon operation")
	}
}

func TestCommandOperationErrorPreservesCause(t *testing.T) {
	cause := errors.New("injected status failure")
	deps := commandDependencies{
		status:     func() error { return cause },
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}

	err := runCommand([]string{"daemon", "status"}, deps)
	if !errors.Is(err, cause) {
		t.Errorf("daemon status error = %v, want injected cause", err)
	}
}

func TestCommandHelpShowsCanonicalTree(t *testing.T) {
	deps := commandDependencies{
		status:     func() error { return nil },
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}

	var rootHelp bytes.Buffer
	deps.output = &rootHelp
	if err := runCommand([]string{"--help"}, deps); err != nil {
		t.Fatalf("root help = %v, want nil", err)
	}
	if got := rootHelp.String(); !strings.Contains(got, "\n  auth ") ||
		!strings.Contains(got, "\n  daemon ") ||
		!strings.Contains(got, "\n  profile ") {
		t.Errorf("root help omitted resource commands:\n%s", got)
	}
	if strings.Contains(rootHelp.String(), "\n  status ") {
		t.Errorf("root help advertised retired status:\n%s", rootHelp.String())
	}

	var daemonHelp bytes.Buffer
	deps.output = &daemonHelp
	if err := runCommand([]string{"daemon", "--help"}, deps); err != nil {
		t.Fatalf("daemon help = %v, want nil", err)
	}
	if got := daemonHelp.String(); !strings.Contains(got, "\n  status ") {
		t.Errorf("daemon help omitted canonical status:\n%s", got)
	}

	var authHelp bytes.Buffer
	deps.output = &authHelp
	if err := runCommand([]string{"auth", "--help"}, deps); err != nil {
		t.Fatalf("auth help = %v, want nil", err)
	}
	if got := authHelp.String(); !strings.Contains(got, "\n  list ") {
		t.Errorf("auth help omitted list:\n%s", got)
	}

	var profileHelp bytes.Buffer
	deps.output = &profileHelp
	if err := runCommand([]string{"profile", "--help"}, deps); err != nil {
		t.Fatalf("profile help = %v, want nil", err)
	}
	if got := profileHelp.String(); !strings.Contains(got, "\n  list ") {
		t.Errorf("profile help omitted list:\n%s", got)
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
	if got := err.Error(); got != "等待 sidraviad 超时" {
		t.Errorf("runStatus = %q, want %q", got, "等待 sidraviad 超时")
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
	expected := fmt.Sprintf("守护进程：运行中（%s） | 版本：%s | 构建：%s | PID：%d\n",
		authoritative.Status, authoritative.ProductVersion, authoritative.BuildID, authoritative.PID)

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

func TestLeafHelpDoesNotDispatch(t *testing.T) {
	leaves := []struct {
		name string
		args []string
	}{
		{"daemon status --help", []string{"daemon", "status", "--help"}},
		{"daemon status -h", []string{"daemon", "status", "-h"}},
		{"auth list --help", []string{"auth", "list", "--help"}},
		{"auth list -h", []string{"auth", "list", "-h"}},
		{"auth start --help", []string{"auth", "start", "--help"}},
		{"auth start -h", []string{"auth", "start", "-h"}},
		{"auth status --help", []string{"auth", "status", "--help"}},
		{"auth status -h", []string{"auth", "status", "-h"}},
		{"auth stop --help", []string{"auth", "stop", "--help"}},
		{"auth stop -h", []string{"auth", "stop", "-h"}},
		{"profile list --help", []string{"profile", "list", "--help"}},
		{"profile list -h", []string{"profile", "list", "-h"}},
	}
	for _, leaf := range leaves {
		t.Run(leaf.name, func(t *testing.T) {
			dispatched := false
			deps := commandDependencies{
				status:      func() error { dispatched = true; return nil },
				authStart:   func(authStartOptions) error { dispatched = true; return nil },
				authStatus:  func(string) error { dispatched = true; return nil },
				authStop:    func(string) error { dispatched = true; return nil },
				authList:    func() error { dispatched = true; return nil },
				profileList: func() error { dispatched = true; return nil },
			}
			var buf bytes.Buffer
			deps.output = &buf
			if err := runCommand(leaf.args, deps); err != nil {
				t.Fatalf("leaf help = %v, want nil", err)
			}
			if dispatched {
				t.Error("leaf help dispatched an operation")
			}
			help := buf.String()
			if !strings.Contains(help, "用法：") {
				t.Errorf("leaf help missing Chinese 用法 heading:\n%s", help)
			}
			for _, english := range []string{"Usage", "Available Commands", "Flags"} {
				if strings.Contains(help, english) {
					t.Errorf("leaf help contains Cobra English heading %q:\n%s", english, help)
				}
			}
		})
	}
}

func TestHelpDoesNotAdvertiseCobraHelpCommand(t *testing.T) {
	deps := commandDependencies{
		status:     func() error { return nil },
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}
	var buf bytes.Buffer
	deps.output = &buf
	if err := runCommand([]string{"--help"}, deps); err != nil {
		t.Fatalf("root help = %v", err)
	}
	help := buf.String()
	if strings.Contains(help, "\n  help ") {
		t.Errorf("root help advertised Cobra help command:\n%s", help)
	}
	if strings.Contains(help, "Help about any command") {
		t.Errorf("root help advertised Cobra help description:\n%s", help)
	}
}

func TestHelpWriterFailureIsObservable(t *testing.T) {
	deps := commandDependencies{
		status:     func() error { return nil },
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}
	cause := errors.New("injected help writer failure")
	deps.output = zeroWriter{err: cause}
	err := runCommand([]string{"--help"}, deps)
	if !errors.Is(err, cause) {
		t.Errorf("root help writer failure = %v, want cause", err)
	}
}

func TestLeafHelpWriterFailureIsObservable(t *testing.T) {
	deps := commandDependencies{
		status:     func() error { return nil },
		authStart:  func(authStartOptions) error { return nil },
		authStatus: func(string) error { return nil },
		authStop:   func(string) error { return nil },
	}
	cause := errors.New("injected leaf help writer failure")
	deps.output = zeroWriter{err: cause}
	err := runCommand([]string{"auth", "start", "--help"}, deps)
	if !errors.Is(err, cause) {
		t.Errorf("leaf help writer failure = %v, want cause", err)
	}
}

// TestHelpRendersCompleteSpacedSections proves the deterministic Chinese help
// specification renders the fixed six sections (when applicable) separated by
// blank lines, without Cobra English headings.
func TestHelpRendersCompleteSpacedSections(t *testing.T) {
	deps := commandDependencies{
		status:      func() error { return nil },
		authStart:   func(authStartOptions) error { return nil },
		authStatus:  func(string) error { return nil },
		authStop:    func(string) error { return nil },
		authList:    func() error { return nil },
		profileList: func() error { return nil },
	}

	t.Run("root has description usage commands examples", func(t *testing.T) {
		var buf bytes.Buffer
		deps.output = &buf
		if err := runCommand([]string{"--help"}, deps); err != nil {
			t.Fatalf("root help = %v", err)
		}
		help := buf.String()
		if !strings.Contains(help, "Sidravia 命令行客户端。\n\n用法：") {
			t.Errorf("root help missing description-usage blank separator:\n%s", help)
		}
		if !strings.Contains(help, "\n\n可用命令：") {
			t.Errorf("root help missing usage-commands blank separator:\n%s", help)
		}
		if !strings.Contains(help, "\n\n示例：") {
			t.Errorf("root help missing commands-examples blank separator:\n%s", help)
		}
		if strings.Contains(help, "参数：") || strings.Contains(help, "选项：") {
			t.Errorf("root help must not contain 参数 or 选项:\n%s", help)
		}
		for _, english := range []string{"Usage", "Available Commands", "Flags", "Examples"} {
			if strings.Contains(help, english) {
				t.Errorf("root help contains Cobra English heading %q:\n%s", english, help)
			}
		}
	})

	t.Run("auth start has options and examples", func(t *testing.T) {
		var buf bytes.Buffer
		deps.output = &buf
		if err := runCommand([]string{"auth", "start", "--help"}, deps); err != nil {
			t.Fatalf("auth start help = %v", err)
		}
		help := buf.String()
		if !strings.Contains(help, "选项：") {
			t.Errorf("auth start help missing 选项 section:\n%s", help)
		}
		if !strings.Contains(help, "--profile <profile-id>") {
			t.Errorf("auth start help missing --profile option:\n%s", help)
		}
		if !strings.Contains(help, "示例：") {
			t.Errorf("auth start help missing 示例 section:\n%s", help)
		}
		if strings.Contains(help, "参数：") {
			t.Errorf("auth start help must not contain 参数:\n%s", help)
		}
	})

	t.Run("auth status has args section", func(t *testing.T) {
		var buf bytes.Buffer
		deps.output = &buf
		if err := runCommand([]string{"auth", "status", "--help"}, deps); err != nil {
			t.Fatalf("auth status help = %v", err)
		}
		help := buf.String()
		if !strings.Contains(help, "参数：") {
			t.Errorf("auth status help missing 参数 section:\n%s", help)
		}
		if !strings.Contains(help, "<session-id>") {
			t.Errorf("auth status help missing <session-id> arg:\n%s", help)
		}
		if strings.Contains(help, "选项：") {
			t.Errorf("auth status help must not contain 选项:\n%s", help)
		}
	})
}
