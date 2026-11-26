package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sidravia/internal/background/daemonctl"
	"sidravia/internal/ipc/contract"
	"strings"
	"testing"
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

func TestParseAuthStartProtocolOverrideFileIsRejected(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "jlu", "--username", "user", "--protocol-override-file"},
		{"--profile", "jlu", "--username", "user", "--protocol-override-file="},
		{"--profile", "jlu", "--username", "user", "--protocol-override-file", "a", "--protocol-override-file", "b"},
		{"--profile", "jlu", "--username", "user", "--protocol-override-file", "a\x00b"},
		{"--session", "s", "--protocol-override-file", "override.json"},
		{"--config", "c", "--protocol-override-file", "override.json"},
	} {
		if _, err := parseAuthStart(args); err == nil {
			t.Fatalf("accepted invalid override arguments %q", args)
		}
	}
}

func TestConfigOverrideFlagsRejectInvalidCombinationsBeforeDispatch(t *testing.T) {
	calls := 0
	deps := commandDependencies{
		configCreate: func(configCreateOptions) error { calls++; return nil },
		configUpdate: func(configUpdateOptions) error { calls++; return nil },
		output:       io.Discard,
	}
	for _, args := range [][]string{
		{"config", "create", "--protocol-override-file"},
		{"config", "create", "--protocol-override-file="},
		{"config", "create", "--protocol-override-file", "a", "--protocol-override-file", "b"},
		{"config", "update", "campus", "--clear-protocol-override=false"},
		{"config", "update", "campus", "--clear-protocol-override", "--protocol-override-file", "override.json"},
		{"config", "update", "campus", "--protocol-override-file"},
	} {
		if err := runCommand(args, deps); err == nil || !strings.HasPrefix(err.Error(), "用法错误") {
			t.Errorf("%q error = %v, want safe usage error", args, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid override commands dispatched %d operations", calls)
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
		{"help", "config", "update"}, {"help", "config", "remove"},
	} {
		var output bytes.Buffer
		deps.output = &output
		if err := runCommand(args, deps); err != nil {
			t.Fatalf("help %q: %v", args, err)
		}
		if !strings.Contains(output.String(), "用法：") {
			t.Fatalf("help %q missing usage:\n%s", args, output.String())
		}
		if strings.Contains(strings.Join(args, " "), "config update") || strings.Contains(strings.Join(args, " "), "config remove") {
			if strings.Count(output.String(), "--allow-insecure-storage") < 2 || !strings.Contains(output.String(), "确认未保护存储风险") {
				t.Fatalf("configuration mutation help omitted usage or consent: %s", output.String())
			}
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
	identity, err := daemonctl.NewIdentity("0.1.0-dev", "cli-build")
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
