package terminal

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/muesli/termenv"

	"sidravia/internal/ipc/contract"
)

// newTestPresentation forces a specific profile for tests. It never queries the
// developer terminal, never enables virtual-terminal processing, and never
// mutates package-global termenv state.
func newTestPresentation(w io.Writer, profile termenv.Profile) *Presentation {
	out := termenv.NewOutput(w, termenv.WithProfile(profile), termenv.WithTTY(false))
	return &Presentation{output: out, profile: profile, restore: func() error { return nil }}
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
	got := Sanitize(input)
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
		if got := Sanitize(c); got != c {
			t.Errorf("Sanitize(%q) = %q, want identity", c, got)
		}
	}
}

func TestSanitizeDynamicTextCannotInjectANSIOrNewline(t *testing.T) {
	got := Sanitize("evil\x1b[2J\r\ninject")
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
	p := NewPresentation(&buf)
	defer p.Close()
	if p.profile != termenv.Ascii {
		t.Errorf("redirected profile = %v, want Ascii", p.profile)
	}
	result := &contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build-1", PID: 42, Status: "running"}
	if err := p.Write(RenderDaemonStatus(p, result)); err != nil {
		t.Fatalf("write = %v", err)
	}
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("redirected output contains ESC: %q", buf.String())
	}
}

func TestRedirectedOutputStaysPlainWithCLICOLORForce(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	var buf bytes.Buffer
	p := NewPresentation(&buf)
	defer p.Close()
	if p.profile != termenv.Ascii {
		t.Errorf("redirected+CLICOLOR_FORCE profile = %v, want Ascii", p.profile)
	}
	_ = p.Write(RenderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "running"}))
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
	defer p.Close()
	_ = p.Write(RenderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "running"}))
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("NO_COLOR output contains ESC: %q", buf.String())
	}
}

func TestForcedANSIProfileStylesTrustedLabelsAndStates(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.ANSI)
	defer p.Close()

	if labelled := p.Label("会话："); !strings.ContainsRune(labelled, '\x1b') {
		t.Errorf("label not styled under ANSI: %q", labelled)
	}
	auth := p.State(SessionStateText("authenticated"), "authenticated")
	if !strings.ContainsRune(auth, '\x1b') || !strings.Contains(auth, "已认证") {
		t.Errorf("authenticated not styled or lost mapped text: %q", auth)
	}
	blocked := p.State(SessionStateText("blocked_by_error"), "blocked_by_error")
	if !strings.ContainsRune(blocked, '\x1b') || !strings.Contains(blocked, "因错误阻塞") {
		t.Errorf("blocked_by_error not styled or lost mapped text: %q", blocked)
	}
	for _, code := range []string{"waiting_for_network", "authenticating", "waiting_before_retry", "stopping"} {
		styled := p.State(SessionStateText(code), code)
		if !strings.ContainsRune(styled, '\x1b') {
			t.Errorf("state %q not styled: %q", code, styled)
		}
	}
	if styled := p.State(SessionStateText("suspended"), "suspended"); strings.ContainsRune(styled, '\x1b') {
		t.Errorf("suspended styled, want plain: %q", styled)
	}
	if styled := p.State(SessionStateText("bogus"), "bogus"); strings.ContainsRune(styled, '\x1b') {
		t.Errorf("unknown state styled, want plain: %q", styled)
	}
}

func TestRenderedBlockWriterFailurePreservesCause(t *testing.T) {
	cause := errors.New("injected writer failure")
	err := WriteSessionResult(zeroWriter{Err: cause}, minimalSessionResult("authenticated"))
	if !errors.Is(err, cause) {
		t.Errorf("WriteSessionResult did not preserve writer cause: %v", err)
	}
}

func TestHelpUsageLinesAreIndentedAndStyledOnlyByProfile(t *testing.T) {
	var plain bytes.Buffer
	p := newTestPresentation(&plain, termenv.Ascii)
	lines := p.HelpUsageLines([]string{"sidraviactl auth start one", "sidraviactl auth start two"})
	if strings.Join(lines, "\n") != "用法：\n  sidraviactl auth start one\n  sidraviactl auth start two" {
		t.Fatalf("usage lines = %#v", lines)
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
	p := &Presentation{
		output:  termenv.NewOutput(&buf, termenv.WithProfile(profile), termenv.WithTTY(false)),
		profile: profile,
		restore: restore,
	}
	if err := p.Complete(RenderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "running"})); err != nil {
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
	p := &Presentation{
		output:  termenv.NewOutput(&bytes.Buffer{}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { restored = true; return nil },
	}
	if err := p.Complete("block\n"); err != nil {
		t.Fatalf("complete = %v", err)
	}
	if !restored {
		t.Error("restore not called after write")
	}
}

func TestPresentationCompleteRestoreFailureAlone(t *testing.T) {
	restoreErr := errors.New("injected restore failure")
	p := &Presentation{
		output:  termenv.NewOutput(&bytes.Buffer{}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { return restoreErr },
	}
	if err := p.Complete("block\n"); !errors.Is(err, restoreErr) {
		t.Errorf("complete did not preserve restore error: %v", err)
	}
}

func TestPresentationCompleteJoinsWriterAndRestoreFailures(t *testing.T) {
	writeErr := errors.New("injected write failure")
	restoreErr := errors.New("injected restore failure")
	p := &Presentation{
		output:  termenv.NewOutput(zeroWriter{Err: writeErr}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { return restoreErr },
	}
	err := p.Complete("block\n")
	if !errors.Is(err, writeErr) {
		t.Errorf("complete did not preserve write error: %v", err)
	}
	if !errors.Is(err, restoreErr) {
		t.Errorf("complete did not preserve restore error: %v", err)
	}
}

func TestWriteErrorCompletionPreservesRestoreFailure(t *testing.T) {
	restoreErr := errors.New("injected restore failure")
	p := &Presentation{
		output:  termenv.NewOutput(&bytes.Buffer{}, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false)),
		profile: termenv.Ascii,
		restore: func() error { return restoreErr },
	}
	if err := p.Complete(ErrorLine(errors.New("boom"))); !errors.Is(err, restoreErr) {
		t.Errorf("WriteError completion did not preserve restore failure: %v", err)
	}
}

func TestRenderConfigurationShowsAutoLoginAutoReconnect(t *testing.T) {
	enabled := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: "automatically_select_latest_available"},
		ConfigurationID:      "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
		AutoLogin: true, AutoReconnect: true,
	}
	got := RenderConfiguration(enabled)
	if !strings.Contains(got, "自动登录：已启用") || !strings.Contains(got, "自动重连：已启用") {
		t.Fatalf("enabled output missing lines: %q", got)
	}

	disabled := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: "automatically_select_latest_available"},
		ConfigurationID:      "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
		AutoLogin: false, AutoReconnect: false,
	}
	got = RenderConfiguration(disabled)
	if !strings.Contains(got, "自动登录：未启用") || !strings.Contains(got, "自动重连：未启用") {
		t.Fatalf("disabled output missing lines: %q", got)
	}
}

func TestCompleteReadHintsStayFixedAndSanitizeIdentifiers(t *testing.T) {
	for availability, hint := range map[string]string{"available": "可用", "profile_unavailable": "机构资料不可用；可删除此配置", "protocol_unavailable": "认证协议不可用；可删除此配置", "override_invalid": "协议设置无效；请修改设置"} {
		value := contract.ConfigurationResult{ConfigurationID: "config\x1b[2J\ninjected", InstitutionProfileID: "missing-profile", Username: "user", RuntimeAvailability: availability}
		output := RenderConfiguration(value)
		if !strings.Contains(output, "可用状态："+hint) || strings.ContainsAny(output, "\x1b") || strings.Contains(output, "\ninjected") {
			t.Fatalf("unsafe hint: %q", output)
		}
	}
	var output bytes.Buffer
	id := "session\x1b[2J\ninjected"
	if err := WriteSessionList(&output, contract.SessionListResult{Sessions: []contract.SessionResult{{AuthenticationSessionID: id, State: "suspended"}}, CleanupRequiredSessionIDs: []string{id}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "需要清理：运行 sidraviactl auth remove 释放此 Session。") || strings.ContainsAny(output.String(), "\x1b") || strings.Contains(output.String(), "\ninjected") {
		t.Fatalf("unsafe cleanup hint: %q", output.String())
	}
}

func minimalSessionResult(state string) contract.SessionResult {
	return contract.SessionResult{
		ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"},
		Revision:       1, Intent: "maintain_authentication", InstitutionDisplayName: "Profile",
		AuthenticationSessionID:  "session-1",
		InstitutionProfileID:     "profile-1",
		AuthenticationProtocolID: "protocol-1",
		AccountName:              "account-name",
		State:                    state,
		UpdatedAt:                "2026-07-26T10:11:12.123456789+08:00",
	}
}

type zeroWriter struct {
	Err error
}

func (writer zeroWriter) Write([]byte) (int, error) {
	return 0, writer.Err
}
