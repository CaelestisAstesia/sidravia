package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

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
	commands := [][]string{
		{"daemon", "status"},
		{"auth", "list"},
		{"auth", "start"},
		{"auth", "status"},
		{"auth", "stop"},
		{"profile", "list"},
	}
	var leaves [][]string
	for _, command := range commands {
		for _, flag := range []string{"--help", "-h"} {
			leaves = append(leaves, append([]string{command[0], command[1]}, flag))
		}
	}
	for _, leaf := range leaves {
		t.Run(strings.Join(leaf, " "), func(t *testing.T) {
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
			if err := runCommand(leaf, deps); err != nil {
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
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"root", []string{"--help"}},
		{"leaf", []string{"auth", "start", "--help"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New("injected help writer failure")
			deps := commandDependencies{
				daemonStatus: func() error { return nil },
				authStart:    func(authStartOptions) error { return nil },
				authStatus:   func(string) error { return nil },
				authStop:     func(string) error { return nil },
				output:       zeroWriter{err: cause},
			}
			if err := runCommand(tc.args, deps); !errors.Is(err, cause) {
				t.Errorf("help writer failure = %v, want cause", err)
			}
		})
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
