package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"sidravia/internal/clientbootstrap"
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

func TestParseAuthStartConfigurationModeIsExclusive(t *testing.T) {
	got, err := parseAuthStart([]string{"--config", "campus"})
	if err != nil || got.configurationID != "campus" {
		t.Fatalf("parse configuration = %#v, %v", got, err)
	}
	for _, args := range [][]string{
		{"--config"}, {"--config", ""}, {"--config", "campus", "--config", "other"},
		{"--config", "campus", "--session", "session-1"},
		{"--config", "campus", "--profile", "jlu", "--username", "user"},
		{"--config", "campus", "--password-stdin"},
	} {
		if _, err := parseAuthStart(args); err == nil {
			t.Fatalf("accepted mixed configuration start: %q", args)
		}
	}
}

func TestConfigurationCommandsDispatchAndHelpNeverDispatch(t *testing.T) {
	var calls []string
	deps := commandDependencies{
		configList:        func() error { calls = append(calls, "list"); return nil },
		configShow:        func(id string) error { calls = append(calls, "show:"+id); return nil },
		configCreate:      func(options configCreateOptions) error { calls = append(calls, "create:"+options.id); return nil },
		configUpdate:      func(options configUpdateOptions) error { calls = append(calls, "update:"+options.id); return nil },
		configSetPassword: func(options configPasswordOptions) error { calls = append(calls, "password:"+options.id); return nil },
		configRemove: func(id string, yes, allow bool) error {
			calls = append(calls, fmt.Sprintf("remove:%s:%t", id, yes))
			return nil
		},
		output: io.Discard,
	}
	for _, args := range [][]string{
		{"config", "list"},
		{"config", "show", "campus"},
		{"config", "create", "--id", "campus", "--profile", "jlu", "--username", "user", "--password-stdin"},
		{"config", "update", "campus", "--name", "Campus"},
		{"config", "set-password", "campus", "--password-stdin"},
		{"config", "remove", "campus", "--yes"},
	} {
		if err := runCommand(args, deps); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
	}
	if got := strings.Join(calls, "|"); got != "list|show:campus|create:campus|update:campus|password:campus|remove:campus:true" {
		t.Fatalf("dispatch = %q", got)
	}
	before := len(calls)
	for _, args := range [][]string{
		{"config", "--help"}, {"config", "create", "--help"}, {"config", "update", "--help"},
		{"config", "set-password", "--help"}, {"config", "remove", "--help"},
	} {
		var output bytes.Buffer
		deps.output = &output
		if err := runCommand(args, deps); err != nil {
			t.Fatalf("help %q: %v", args, err)
		}
		if !strings.Contains(output.String(), "用法：") {
			t.Fatalf("help %q missing usage:\n%s", args, output.String())
		}
		if strings.Join(args, " ") == "config create --help" {
			for _, expected := range []string{"自动绑定", "--interface-id", "--local-ipv4"} {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("config create help omitted %q:\n%s", expected, output.String())
				}
			}
		}
	}
	if len(calls) != before {
		t.Fatal("configuration help dispatched an operation")
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
			"sidraviactl auth start --profile <profile-id> --username <username> [--password-stdin]",
			"sidraviactl auth start --session <session-id>",
		}},
		{[]string{"auth", "restart", "--help"}, []string{"sidraviactl auth restart <session-id>"}},
		{[]string{"auth", "remove", "--help"}, []string{"sidraviactl auth remove <session-id>"}},
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
		{"bogus"},
		{"status", "extra"},
		{"daemon", "status", "extra"},
		{"daemon", "start", "--log-level", "verbose"},
		{"daemon", "start", "extra"},
		{"daemon", "stop", "extra"},
		{"login"},
		{"logout"},
		{"list"},
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
		if got := err.Error(); !strings.HasPrefix(got, "用法错误，请运行 sidraviactl help") {
			t.Errorf("Run error = %q, want safe help guidance", got)
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
				daemonStatus: func() error {
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
		daemonStatus: func() error {
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
	if err.Error() != "命令已迁移，请使用 sidraviactl daemon status" {
		t.Errorf("retired status error = %q", err)
	}
	if dispatched {
		t.Error("retired status dispatched daemon operation")
	}
}

func TestCommandOperationErrorPreservesCause(t *testing.T) {
	cause := errors.New("injected status failure")
	deps := commandDependencies{
		daemonStatus: func() error { return cause },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
	}

	err := runCommand([]string{"daemon", "status"}, deps)
	if !errors.Is(err, cause) {
		t.Errorf("daemon status error = %v, want injected cause", err)
	}
}

func TestDaemonCommandFailuresUseSafePublicMessages(t *testing.T) {
	tests := []struct {
		name string
		args []string
		set  func(*commandDependencies, error)
		want string
	}{
		{
			name: "status",
			args: []string{"daemon", "status"},
			set: func(deps *commandDependencies, err error) {
				deps.daemonStatus = func() error { return err }
			},
			want: daemonStatusFailureMessage,
		},
		{
			name: "start",
			args: []string{"daemon", "start"},
			set: func(deps *commandDependencies, err error) {
				deps.daemonStart = func(string) error { return err }
			},
			want: daemonStartFailureMessage,
		},
		{
			name: "stop",
			args: []string{"daemon", "stop"},
			set: func(deps *commandDependencies, err error) {
				deps.daemonStop = func() error { return err }
			},
			want: daemonStopFailureMessage,
		},
		{
			name: "restart",
			args: []string{"daemon", "restart"},
			set: func(deps *commandDependencies, err error) {
				deps.daemonRestart = func(string) error { return err }
			},
			want: daemonRestartFailureMessage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("injected lifecycle cause")
			privateMarker := `C:\Users\private\sidraviad.exe: access denied`
			wrapped := fmt.Errorf("outer lifecycle context: %w", fmt.Errorf("%s: %w", privateMarker, cause))
			deps := commandDependencies{output: io.Discard}
			test.set(&deps, wrapped)

			err := runCommand(test.args, deps)
			if err == nil || !errors.Is(err, cause) {
				t.Fatalf("command error = %v, want wrapped cause", err)
			}
			var output bytes.Buffer
			if writeErr := WriteError(&output, err); writeErr != nil {
				t.Fatalf("WriteError() error = %v", writeErr)
			}
			want := "sidraviactl：错误：" + test.want + "\n"
			if output.String() != want || strings.Contains(output.String(), privateMarker) || strings.Contains(output.String(), "outer lifecycle context") {
				t.Fatalf("WriteError() = %q, want %q", output.String(), want)
			}
		})
	}
}

func TestDaemonStopAmbiguityKeepsItsTypedSafeMessage(t *testing.T) {
	cause := errors.New("private transport cause")
	privateMarker := `C:\Users\private\sidraviad.exe: connect failed`
	ambiguity := "守护进程：停止请求尚未发送，无法确认当前状态；请运行 sidraviactl daemon status 后重试"
	deps := commandDependencies{
		output: io.Discard,
		daemonStop: func() error {
			return fmt.Errorf("outer bootstrap context: %w", wrapSafeOperation(ambiguity, fmt.Errorf("%s: %w", privateMarker, cause)))
		},
	}
	err := runCommand([]string{"daemon", "stop"}, deps)
	if err == nil || !errors.Is(err, cause) {
		t.Fatalf("daemon stop error = %v, want preserved transport cause", err)
	}
	var output bytes.Buffer
	if writeErr := WriteError(&output, err); writeErr != nil {
		t.Fatalf("WriteError() error = %v", writeErr)
	}
	want := "sidraviactl：错误：" + ambiguity + "\n"
	if output.String() != want || strings.Contains(output.String(), privateMarker) || strings.Contains(output.String(), "outer bootstrap context") {
		t.Fatalf("WriteError() = %q, want %q", output.String(), want)
	}
}

func TestCommandHelpShowsCanonicalTree(t *testing.T) {
	deps := commandDependencies{
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
	}

	var rootHelp bytes.Buffer
	deps.output = &rootHelp
	if err := runCommand([]string{"--help"}, deps); err != nil {
		t.Fatalf("root help = %v, want nil", err)
	}
	if got := rootHelp.String(); !strings.Contains(got, "\n  auth ") ||
		!strings.Contains(got, "\n  daemon ") ||
		!strings.Contains(got, "\n  profile ") ||
		!strings.Contains(got, "\n  diagnostics ") {
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

func TestUnifiedHelpEntrypointsAreIdenticalAndNeverDispatch(t *testing.T) {
	dispatched := 0
	deps := commandDependencies{
		daemonStatus: func() error { dispatched++; return nil },
		daemonStart:  func(string) error { dispatched++; return nil },
		daemonStop:   func() error { dispatched++; return nil },
		authStart:    func(authStartOptions) error { dispatched++; return nil },
		authList:     func() error { dispatched++; return nil },
		profileList:  func() error { dispatched++; return nil },
	}
	assertSame := func(entries ...[]string) {
		t.Helper()
		var want string
		for index, args := range entries {
			var output bytes.Buffer
			deps.output = &output
			if err := runCommand(args, deps); err != nil {
				t.Fatalf("%q: %v", args, err)
			}
			if index == 0 {
				want = output.String()
			} else if output.String() != want {
				t.Fatalf("%q output differs:\n%s\nwant:\n%s", args, output.String(), want)
			}
		}
	}
	assertSame(nil, []string{"--help"}, []string{"-h"}, []string{"help"})
	assertSame([]string{"daemon"}, []string{"daemon", "--help"}, []string{"help", "daemon"})
	assertSame([]string{"auth"}, []string{"auth", "--help"}, []string{"help", "auth"})
	assertSame([]string{"auth", "start", "--help"}, []string{"auth", "start", "-h"}, []string{"help", "auth", "start"})
	if dispatched != 0 {
		t.Fatalf("help dispatched %d operations", dispatched)
	}
}

func TestHelpPathRejectsUnknownHiddenAndExtraTokensSafely(t *testing.T) {
	for _, args := range [][]string{
		{"help", "missing-marker"},
		{"help", "status"},
		{"help", "auth", "start", "extra-marker"},
	} {
		err := runCommand(args, commandDependencies{output: io.Discard})
		if err != errCommandUsage {
			t.Fatalf("%q error = %v", args, err)
		}
		for _, marker := range []string{"missing-marker", "extra-marker"} {
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("%q echoed marker in %q", args, err)
			}
		}
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
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { t.Fatal("invalid start dispatched"); return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
	}
	for _, args := range tests {
		err := runCommand(args, deps)
		if err == nil {
			t.Error("invalid auth start returned nil")
			continue
		}
		if !strings.HasPrefix(err.Error(), "用法错误，请运行 sidraviactl help auth start") {
			t.Errorf("invalid auth start error = %q, want nearest safe help", err)
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
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { t.Fatal("invalid status dispatched"); return nil },
		authStop:     func(string) error { t.Fatal("invalid stop dispatched"); return nil },
	}
	for _, args := range tests {
		err := runCommand(args, deps)
		if err == nil {
			t.Error("invalid session command returned nil")
			continue
		}
		if !strings.HasPrefix(err.Error(), "用法错误，请运行 sidraviactl help auth ") {
			t.Errorf("invalid session command error = %q, want nearest safe help", err)
		}
		if strings.Contains(err.Error(), "marker") {
			t.Error("usage error contains supplied session marker")
		}
	}
}

func TestAuthStartHelpIncludesStatusNextStep(t *testing.T) {
	var output bytes.Buffer
	deps := commandDependencies{output: &output}
	if err := runCommand([]string{"auth", "start", "--help"}, deps); err != nil {
		t.Fatalf("auth start help = %v", err)
	}
	want := "下一步：使用返回的 Session ID 运行 sidraviactl auth status <session-id>；不知道 ID 时先运行 sidraviactl auth list。"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("auth start help omitted actionable next step %q:\n%s", want, output.String())
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
				daemonStatus: func() error { dispatched = true; return nil },
				authStart:    func(authStartOptions) error { dispatched = true; return nil },
				authStatus:   func(string) error { dispatched = true; return nil },
				authStop:     func(string) error { dispatched = true; return nil },
				authList:     func() error { dispatched = true; return nil },
				profileList:  func() error { dispatched = true; return nil },
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
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
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
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
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
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
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
		daemonStatus: func() error { return nil },
		authStart:    func(authStartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
		authList:     func() error { return nil },
		profileList:  func() error { return nil },
	}

	t.Run("root has description usage commands examples", func(t *testing.T) {
		var buf bytes.Buffer
		deps.output = &buf
		if err := runCommand([]string{"--help"}, deps); err != nil {
			t.Fatalf("root help = %v", err)
		}
		help := buf.String()
		if !strings.Contains(help, "Sidravia 命令行客户端。\n\n用法：\n  sidraviactl <command>") {
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

	t.Run("group usage is layered", func(t *testing.T) {
		var buf bytes.Buffer
		deps.output = &buf
		if err := runCommand([]string{"daemon"}, deps); err != nil {
			t.Fatal(err)
		}
		help := buf.String()
		if !strings.Contains(help, "用法：\n  sidraviactl daemon <command>") || strings.Contains(help, " | ") {
			t.Fatalf("daemon help is not layered:\n%s", help)
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
		if !strings.Contains(help, "用法：\n  sidraviactl auth start --profile <profile-id> --username <username> [--password-stdin]\n  sidraviactl auth start --session <session-id>") {
			t.Errorf("auth start usages are not separate lines:\n%s", help)
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

func TestNoSettingsCommandExists(t *testing.T) {
	deps := commandDependencies{
		daemonStatus:      func() error { return nil },
		daemonStart:       func(string) error { return nil },
		daemonStop:        func() error { return nil },
		daemonRestart:     func(string) error { return nil },
		authStart:         func(authStartOptions) error { return nil },
		authStatus:        func(string) error { return nil },
		authStop:          func(string) error { return nil },
		authRestart:       func(string) error { return nil },
		authRemove:        func(string) error { return nil },
		authList:          func() error { return nil },
		profileList:       func() error { return nil },
		configList:        func() error { return nil },
		configShow:        func(string) error { return nil },
		configCreate:      func(configCreateOptions) error { return nil },
		configUpdate:      func(configUpdateOptions) error { return nil },
		configSetPassword: func(configPasswordOptions) error { return nil },
		configRemove:      func(string, bool, bool) error { return nil },
		output:            io.Discard,
	}
	if err := runCommand([]string{"settings"}, deps); err == nil {
		t.Fatal("settings command unexpectedly accepted")
	}
	if err := runCommand([]string{"config", "settings"}, deps); err == nil {
		t.Fatal("config settings subcommand unexpectedly accepted")
	}
}

func TestDefaultCommandDependenciesCaptureOneIdentityForAllOperations(t *testing.T) {
	identity, err := clientbootstrap.NewIdentity("0.1.0-dev", "cli-build")
	if err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies(identity)
	if deps.identity != identity {
		t.Fatalf("identity = %+v, want %+v", deps.identity, identity)
	}
	for name, operation := range map[string]any{
		"daemon status": deps.daemonStatus, "daemon start": deps.daemonStart, "daemon stop": deps.daemonStop, "daemon restart": deps.daemonRestart,
		"auth start": deps.authStart, "auth status": deps.authStatus, "auth stop": deps.authStop, "auth restart": deps.authRestart, "auth remove": deps.authRemove,
		"auth list": deps.authList, "profile list": deps.profileList,
		"diagnostics export": deps.diagnosticsExport,
		"config list":        deps.configList, "config show": deps.configShow, "config create": deps.configCreate, "config update": deps.configUpdate,
		"config password": deps.configSetPassword, "config remove": deps.configRemove,
	} {
		if operation == nil {
			t.Errorf("%s dependency is nil", name)
		}
	}
}

func TestNetworkCommandAndHelpOnlyDispatchQuery(t *testing.T) {
	calls := 0
	var output bytes.Buffer
	deps := commandDependencies{networkInterfaces: func() error { calls++; return nil }, output: &output}
	if err := runCommand([]string{"network", "interfaces"}, deps); err != nil || calls != 1 {
		t.Fatalf("query dispatch = %d %v", calls, err)
	}
	var leafHelp string
	for _, args := range [][]string{{"network"}, {"help", "network"}, {"network", "--help"}, {"network", "-h"}, {"help", "network", "interfaces"}, {"network", "interfaces", "--help"}, {"network", "interfaces", "-h"}} {
		output.Reset()
		if err := runCommand(args, deps); err != nil {
			t.Fatalf("%q help: %v", args, err)
		}
		if !strings.Contains(output.String(), "用法：") || calls != 1 {
			t.Fatal("help dispatched query or lacked usage")
		}
		if strings.Contains(output.String(), "sidraviactl network interfaces\n") {
			if leafHelp == "" {
				leafHelp = output.String()
			} else if leafHelp != output.String() {
				t.Fatal("leaf help differs")
			}
		}
	}
	for _, args := range [][]string{{"network", "interfaces", "--json"}, {"network", "interfaces", "extra"}} {
		if err := runCommand(args, deps); err == nil || calls != 1 {
			t.Fatal("invalid query invocation dispatched")
		}
	}
}

func TestNetworkDiagnoseCommandExclusiveSelectorAndOperationFreeHelp(t *testing.T) {
	calls := 0
	deps := commandDependencies{networkDiagnose: func(request contract.NetworkDiagnosePayload) error {
		calls++
		if request.ConfigurationID != "c" && request.SessionID != "s" {
			t.Fatal("selector changed")
		}
		return nil
	}, output: io.Discard}
	for _, args := range [][]string{{"network", "diagnose", "--config", "c"}, {"network", "diagnose", "--session", "s", "--probe"}} {
		if err := runCommand(args, deps); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("valid commands not dispatched")
	}
	for _, args := range [][]string{{"network", "diagnose", "--probe"}, {"network", "diagnose", "--config", ""}, {"network", "diagnose", "--config", "c", "--session", "s"}, {"network", "diagnose", "--config", "c", "extra"}} {
		if err := runCommand(args, deps); err == nil {
			t.Fatal("invalid selector accepted")
		}
	}
	for _, args := range [][]string{{"network"}, {"network", "diagnose"}, {"help", "network", "diagnose"}, {"network", "diagnose", "-h"}, {"network", "diagnose", "--help"}} {
		var output bytes.Buffer
		deps.output = &output
		if err := runCommand(args, deps); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "用法：") || !strings.Contains(output.String(), "diagnose") {
			t.Fatal("static help omitted")
		}
	}
	if calls != 2 {
		t.Fatal("help or invalid selectors dispatched")
	}
}
