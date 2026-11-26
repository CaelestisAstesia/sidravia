package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/muesli/termenv"

	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

// newTestPresentation forces a specific profile for tests. It never queries the
// developer terminal, never enables virtual-terminal processing, and never
// mutates package-global termenv state.
func newTestPresentation(w io.Writer, profile termenv.Profile) *presentation {
	out := termenv.NewOutput(w, termenv.WithProfile(profile), termenv.WithTTY(false))
	return &presentation{output: out, profile: profile, restore: func() error { return nil }}
}

// staticEnviron is a deterministic termenv.Environ for capability tests. It
// never touches the developer's real terminal environment.
type staticEnviron struct {
	vars map[string]string
}

func (e *staticEnviron) Environ() []string      { return nil }
func (e *staticEnviron) Getenv(k string) string { return e.vars[k] }

func TestSanitizeDynamicTextReplacesControlCharacters(t *testing.T) {
	input := "a\x1b[2Jb\rc\nd\te\x00f\x7fg\x80h\x9fi"
	got := sanitizeDynamicText(input)
	for _, r := range got {
		if isControlRune(r) {
			t.Errorf("sanitized text contains control rune %U in %q", r, got)
		}
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "i") {
		t.Errorf("sanitized text lost ordinary characters: %q", got)
	}
}

func TestSanitizeDynamicTextPreservesOrdinaryText(t *testing.T) {
	cases := []string{
		"吉林大学",
		"session-1",
		"2026-07-26T10:11:12.123456789+08:00",
		"192.0.2.25",
		"drcom-5.2.0-d",
		"普通文本 with spaces, punctuation!",
		"",
	}
	for _, c := range cases {
		if got := sanitizeDynamicText(c); got != c {
			t.Errorf("sanitizeDynamicText(%q) = %q, want identity", c, got)
		}
	}
}

func TestSanitizeDynamicTextCannotInjectANSIOrNewline(t *testing.T) {
	got := sanitizeDynamicText("evil\x1b[2J\r\ninject")
	if strings.Contains(got, "\x1b[2J") {
		t.Errorf("ANSI sequence survived sanitization: %q", got)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("newline survived sanitization: %q", got)
	}
}

func TestResolveColorProfileGate(t *testing.T) {
	// A redirected/non-interactive writer has Ascii capability; output stays
	// plain regardless of CLICOLOR_FORCE (which would make envProfile ANSI).
	if got := resolveColorProfile(termenv.Ascii, termenv.ANSI); got != termenv.Ascii {
		t.Errorf("redirected profile = %v, want Ascii", got)
	}
	// A color terminal with NO_COLOR (envProfile Ascii) stays plain.
	if got := resolveColorProfile(termenv.TrueColor, termenv.Ascii); got != termenv.Ascii {
		t.Errorf("NO_COLOR profile = %v, want Ascii", got)
	}
	// A color terminal without NO_COLOR keeps color.
	if got := resolveColorProfile(termenv.TrueColor, termenv.TrueColor); got != termenv.TrueColor {
		t.Errorf("color profile = %v, want TrueColor", got)
	}
}

func TestEnvColorProfileHonorsNoColor(t *testing.T) {
	env := &staticEnviron{vars: map[string]string{"NO_COLOR": "1", "TERM": "xterm-256color"}}
	out := termenv.NewOutput(io.Discard, termenv.WithEnvironment(env), termenv.WithTTY(true))
	// With a color TERM and a TTY, ColorProfile is non-Ascii, but NO_COLOR
	// forces EnvColorProfile to Ascii.
	if cp := out.ColorProfile(); cp == termenv.Ascii {
		t.Errorf("ColorProfile = Ascii, want color capability for xterm-256color TTY")
	}
	if got := out.EnvColorProfile(); got != termenv.Ascii {
		t.Errorf("EnvColorProfile with NO_COLOR = %v, want Ascii", got)
	}
}

func TestAsciiPresentationContainsNoESC(t *testing.T) {
	var buf bytes.Buffer
	p := newPresentation(&buf)
	defer p.close()
	if p.profile != termenv.Ascii {
		t.Errorf("redirected profile = %v, want Ascii", p.profile)
	}
	result := &contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build-1", PID: 42, Status: "running"}
	if err := p.write(renderDaemonStatus(p, result)); err != nil {
		t.Fatalf("write = %v", err)
	}
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("redirected output contains ESC: %q", buf.String())
	}
}

func TestRedirectedOutputStaysPlainWithCLICOLORForce(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	var buf bytes.Buffer
	p := newPresentation(&buf)
	defer p.close()
	if p.profile != termenv.Ascii {
		t.Errorf("redirected+CLICOLOR_FORCE profile = %v, want Ascii", p.profile)
	}
	_ = p.write(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "running"}))
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("redirected+CLICOLOR_FORCE output contains ESC: %q", buf.String())
	}
}

func TestNoColorSelectsPlainOutput(t *testing.T) {
	// Simulate a color-capable terminal whose EnvColorProfile is Ascii because
	// of NO_COLOR; the resolved profile must be Ascii and output stays plain.
	profile := resolveColorProfile(termenv.TrueColor, termenv.Ascii)
	var buf bytes.Buffer
	p := newTestPresentation(&buf, profile)
	defer p.close()
	_ = p.write(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "running"}))
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("NO_COLOR output contains ESC: %q", buf.String())
	}
}

func TestForcedANSIProfileStylesTrustedLabelsAndStates(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.ANSI)
	defer p.close()

	if labelled := p.label("会话："); !strings.ContainsRune(labelled, '\x1b') {
		t.Errorf("label not styled under ANSI: %q", labelled)
	}
	auth := p.state(sessionStateText("authenticated"), "authenticated")
	if !strings.ContainsRune(auth, '\x1b') || !strings.Contains(auth, "已认证") {
		t.Errorf("authenticated not styled or lost mapped text: %q", auth)
	}
	blocked := p.state(sessionStateText("blocked_by_error"), "blocked_by_error")
	if !strings.ContainsRune(blocked, '\x1b') || !strings.Contains(blocked, "因错误阻塞") {
		t.Errorf("blocked_by_error not styled or lost mapped text: %q", blocked)
	}
	for _, code := range []string{"waiting_for_network", "authenticating", "waiting_before_retry", "stopping"} {
		styled := p.state(sessionStateText(code), code)
		if !strings.ContainsRune(styled, '\x1b') {
			t.Errorf("state %q not styled: %q", code, styled)
		}
	}
	if styled := p.state(sessionStateText("suspended"), "suspended"); strings.ContainsRune(styled, '\x1b') {
		t.Errorf("suspended styled, want plain: %q", styled)
	}
	if styled := p.state(sessionStateText("bogus"), "bogus"); strings.ContainsRune(styled, '\x1b') {
		t.Errorf("unknown state styled, want plain: %q", styled)
	}
}

func TestRenderedBlockWriterFailurePreservesCause(t *testing.T) {
	cause := errors.New("injected writer failure")
	err := writeSessionResult(zeroWriter{err: cause}, minimalSessionResult("authenticated"))
	if !errors.Is(err, cause) {
		t.Errorf("writeSessionResult did not preserve writer cause: %v", err)
	}
}

func TestHelpUsesChineseHeadingsAndCanonicalTree(t *testing.T) {
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

func TestHelpUsageLinesAreIndentedAndStyledOnlyByProfile(t *testing.T) {
	var plain bytes.Buffer
	p := newTestPresentation(&plain, termenv.Ascii)
	lines := p.helpUsageLines([]string{"sidraviactl auth start one", "sidraviactl auth start two"})
	if strings.Join(lines, "\n") != "用法：\n  sidraviactl auth start one\n  sidraviactl auth start two" {
		t.Fatalf("usage lines = %#v", lines)
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

func TestResolveVTEnableFailureForcesAsciiAndNoESC(t *testing.T) {
	profile, restore := resolveVTEnable(termenv.TrueColor, nil, errors.New("vt enable failed"))
	if profile != termenv.Ascii {
		t.Errorf("VT-enable failure profile = %v, want Ascii", profile)
	}
	if restore == nil {
		t.Fatal("VT-enable failure restore = nil, want no-op restore")
	}
	if err := restore(); err != nil {
		t.Errorf("no-op restore returned error: %v", err)
	}
	var buf bytes.Buffer
	p := &presentation{
		output:  termenv.NewOutput(&buf, termenv.WithProfile(profile), termenv.WithTTY(false)),
		profile: profile,
		restore: restore,
	}
	if err := p.complete(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "running"})); err != nil {
		t.Fatalf("complete = %v", err)
	}
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("VT-enable failure path emitted ESC: %q", buf.String())
	}
}

func TestResolveVTEnableSuccessKeepsProfileAndRestore(t *testing.T) {
	called := false
	realRestore := func() error { called = true; return nil }
	profile, restore := resolveVTEnable(termenv.TrueColor, realRestore, nil)
	if profile != termenv.TrueColor {
		t.Errorf("VT-enable success profile = %v, want TrueColor", profile)
	}
	if restore == nil {
		t.Fatal("VT-enable success restore = nil, want real restore")
	}
	if err := restore(); err != nil {
		t.Errorf("restore returned error: %v", err)
	}
	if !called {
		t.Error("VT-enable success did not install the real restore")
	}
}

func TestPresentationCompleteRestoresAfterWrite(t *testing.T) {
	restored := false
	p := &presentation{
		output:  termenv.NewOutput(&bytes.Buffer{}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { restored = true; return nil },
	}
	if err := p.complete("block\n"); err != nil {
		t.Fatalf("complete = %v", err)
	}
	if !restored {
		t.Error("restore not called after write")
	}
}

func TestPresentationCompleteRestoreFailureAlone(t *testing.T) {
	restoreErr := errors.New("injected restore failure")
	p := &presentation{
		output:  termenv.NewOutput(&bytes.Buffer{}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { return restoreErr },
	}
	if err := p.complete("block\n"); !errors.Is(err, restoreErr) {
		t.Errorf("complete did not preserve restore error: %v", err)
	}
}

func TestPresentationCompleteJoinsWriterAndRestoreFailures(t *testing.T) {
	writeErr := errors.New("injected write failure")
	restoreErr := errors.New("injected restore failure")
	p := &presentation{
		output:  termenv.NewOutput(zeroWriter{err: writeErr}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { return restoreErr },
	}
	err := p.complete("block\n")
	if !errors.Is(err, writeErr) {
		t.Errorf("complete did not preserve write error: %v", err)
	}
	if !errors.Is(err, restoreErr) {
		t.Errorf("complete did not preserve restore error: %v", err)
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
	if err := WriteError(zeroWriter{err: cause}, errors.New("boom")); !errors.Is(err, cause) {
		t.Errorf("WriteError did not preserve writer failure: %v", err)
	}
}

func TestWriteErrorCompletionPreservesRestoreFailure(t *testing.T) {
	restoreErr := errors.New("injected restore failure")
	p := &presentation{
		output:  termenv.NewOutput(&bytes.Buffer{}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { return restoreErr },
	}
	if err := p.complete(writeErrorLine(errors.New("boom"))); !errors.Is(err, restoreErr) {
		t.Errorf("WriteError completion did not preserve restore failure: %v", err)
	}
}

func TestRenderConfigurationShowsAutoLoginAutoReconnect(t *testing.T) {
	enabled := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
		AutoLogin: true, AutoReconnect: true,
	}
	got := renderConfiguration(enabled)
	if !strings.Contains(got, "自动登录：已启用") || !strings.Contains(got, "自动重连：已启用") {
		t.Fatalf("enabled output missing lines: %q", got)
	}

	disabled := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
		AutoLogin: false, AutoReconnect: false,
	}
	got = renderConfiguration(disabled)
	if !strings.Contains(got, "自动登录：未启用") || !strings.Contains(got, "自动重连：未启用") {
		t.Fatalf("disabled output missing lines: %q", got)
	}
}

func TestCompleteReadHintsStayFixedAndSanitizeIdentifiers(t *testing.T) {
	for availability, hint := range map[string]string{"available": "可用", "profile_unavailable": "机构资料不可用；可删除此配置", "protocol_unavailable": "认证协议不可用；可删除此配置", "override_invalid": "协议设置无效；请修改设置"} {
		value := contract.ConfigurationResult{ConfigurationID: "config\x1b[2J\ninjected", InstitutionProfileID: "missing-profile", Username: "user", RuntimeAvailability: availability}
		output := renderConfiguration(value)
		if !strings.Contains(output, "可用状态："+hint) || strings.ContainsAny(output, "\x1b") || strings.Contains(output, "\ninjected") {
			t.Fatalf("unsafe hint: %q", output)
		}
	}
	var output bytes.Buffer
	id := "session\x1b[2J\ninjected"
	if err := writeSessionList(&output, contract.SessionListResult{Sessions: []contract.SessionResult{{AuthenticationSessionID: id, State: "suspended"}}, CleanupRequiredSessionIDs: []string{id}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "需要清理：运行 sidraviactl auth remove 释放此 Session。") || strings.ContainsAny(output.String(), "\x1b") || strings.Contains(output.String(), "\ninjected") {
		t.Fatalf("unsafe cleanup hint: %q", output.String())
	}
}
