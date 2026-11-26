package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/cli/internal/authcmd"
	"sidravia/internal/cli/internal/clitest"
	"sidravia/internal/cli/internal/terminal"
)

func TestHelpUsesChineseHeadingsAndCanonicalTree(t *testing.T) {
	deps := commandDependencies{
		daemonStatus: func() error { return nil },
		authStart:    func(authcmd.StartOptions) error { return nil },
		authStatus:   func(string) error { return nil },
		authStop:     func(string) error { return nil },
	}
	var buf bytes.Buffer
	deps.output = &buf
	if err := runCommand([]string{"--help"}, deps); err != nil {
		t.Fatalf("root help = %v", err)
	}
	help := buf.String()
	if !strings.Contains(help, "用法：") || !strings.Contains(help, "可用命令：") {
		t.Errorf("help missing Chinese headings:\n%s", help)
	}
	for _, english := range []string{"Usage", "Available Commands", "Flags", "Additional help topics"} {
		if strings.Contains(help, english) {
			t.Errorf("help contains Cobra English heading %q:\n%s", english, help)
		}
	}
	if strings.Contains(help, "\n  status ") {
		t.Errorf("root help advertised retired status:\n%s", help)
	}
}

func TestStaticUsageDoesNotEchoInvalidMarkers(t *testing.T) {
	err := Run([]string{"auth", "start", "--profile", "profile-marker", "--username", "user-marker", "--bogus"})
	if err == nil {
		t.Fatal("Run returned nil, want usage error")
	}
	if err.Error() != "用法错误，请运行 sidraviactl help auth start 查看帮助" {
		t.Errorf("usage error = %q", err.Error())
	}
	for _, marker := range []string{"profile-marker", "user-marker"} {
		if strings.Contains(err.Error(), marker) {
			t.Errorf("usage error echoes supplied marker %q", marker)
		}
	}
}

func TestWriteErrorSanitizesAndHidesCause(t *testing.T) {
	cause := errors.New("injected underlying cause")
	safe := terminal.Failure("连接 sidraviad", cause)
	var buf bytes.Buffer
	if err := WriteError(&buf, safe); err != nil {
		t.Fatalf("WriteError = %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "sidraviactl：错误：连接 sidraviad\n") {
		t.Errorf("WriteError output = %q, want Chinese prefix with safe label only", out)
	}
	if strings.Contains(out, "injected underlying cause") {
		t.Errorf("WriteError exposed underlying cause: %q", out)
	}
}

func TestWriteErrorSanitizesControlCharacters(t *testing.T) {
	evil := errors.New("evil\x1b[2J\rinject")
	var buf bytes.Buffer
	if err := WriteError(&buf, evil); err != nil {
		t.Fatalf("WriteError = %v", err)
	}
	out := buf.String()
	if strings.ContainsAny(out, "\x1b\r") {
		t.Errorf("WriteError survived control characters: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("WriteError line count = %d, want 1: %q", strings.Count(out, "\n"), out)
	}
	if !strings.HasPrefix(out, "sidraviactl：错误：") {
		t.Errorf("WriteError missing Chinese prefix: %q", out)
	}
}

func TestWriteErrorPreservesWriterFailure(t *testing.T) {
	cause := errors.New("injected write failure")
	if err := WriteError(clitest.ZeroWriter{Err: cause}, errors.New("boom")); !errors.Is(err, cause) {
		t.Errorf("WriteError did not preserve writer failure: %v", err)
	}
}
