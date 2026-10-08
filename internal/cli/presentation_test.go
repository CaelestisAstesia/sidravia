package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/muesli/termenv"

	"sidravia/internal/ipc/contract"
)

func TestRenderSessionRemovedSanitizesID(t *testing.T) {
	got := renderSessionRemoved(&contract.SessionRemoveResult{SessionID: "session-1\nescape"})
	if got != "Session 已删除：session-1�escape\n" {
		t.Fatalf("removed output = %q", got)
	}
}

func TestSessionRemovedPlainAndForcedColorContracts(t *testing.T) {
	result := &contract.SessionRemoveResult{SessionID: "session-1\x1b[2J\ninjected"}
	block := renderSessionRemoved(result)
	if block != "Session 已删除：session-1�[2J�injected\n" {
		t.Fatalf("plain remove output=%q", block)
	}
	if strings.Count(block, "\n") != 1 || strings.ContainsRune(block, '\x1b') {
		t.Fatalf("remove output allowed injection: %q", block)
	}
	var output bytes.Buffer
	p := newTestPresentation(&output, termenv.ANSI)
	if err := p.complete(block); err != nil {
		t.Fatal(err)
	}
	if output.String() != block {
		t.Fatalf("forced-color remove changed unstyled contract: %q", output.String())
	}
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

func TestSessionStateMappings(t *testing.T) {
	cases := map[string]string{
		"suspended":            "已暂停",
		"waiting_for_network":  "等待可用网络",
		"authenticating":       "正在认证",
		"authenticated":        "已认证",
		"waiting_before_retry": "等待重试",
		"blocked_by_error":     "因错误阻塞",
		"stopping":             "正在停止",
	}
	for code, want := range cases {
		if got := sessionStateText(code); got != want {
			t.Errorf("sessionStateText(%q) = %q, want %q", code, got, want)
		}
	}
	if got := sessionStateText("bogus"); got != "未知状态" {
		t.Errorf("unknown state = %q, want plain fallback", got)
	}
}

func TestStateReasonMappings(t *testing.T) {
	cases := map[string]string{
		"network_unavailable":            "没有可用网络",
		"runtime_definition_unavailable": "认证运行配置不可用",
		"protocol_run_creation_failed":   "无法创建认证协议运行",
		"protocol_run_failed":            "认证协议运行失败",
		"protocol_contract_violated":     "认证协议违反内部契约",
		"automatic_reconnect_disabled":   "自动重连已关闭",
	}
	for code, want := range cases {
		if got := sessionStateReasonText(code); got != want {
			t.Errorf("sessionStateReasonText(%q) = %q, want %q", code, got, want)
		}
	}
	if got := sessionStateReasonText("bogus"); got != "未知原因" {
		t.Errorf("unknown reason = %q, want plain fallback", got)
	}
}

func TestSessionDetailExplainsAutoReconnectDisabled(t *testing.T) {
	result := minimalSessionResult("blocked_by_error")
	result.StateReason = &contract.SessionStateReason{Code: "automatic_reconnect_disabled", Description: "automatic reconnect disabled"}
	result.LastAuthenticationFailure = &contract.SessionAuthenticationFailure{
		Code:                   "network_timeout",
		Description:            "network timeout",
		HandlingRecommendation: "block_until_explicit_restart_or_relevant_input_change",
	}

	var output bytes.Buffer
	if err := writeSessionResult(&output, result); err != nil {
		t.Fatalf("writeSessionResult = %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"原因：自动重连已关闭（automatic_reconnect_disabled）",
		"处理建议：请检查账号、机构配置和网络后停止并重新启动该 Session（block_until_explicit_restart_or_relevant_input_change）",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %q", want, got)
		}
	}
	for _, unwanted := range []string{"将自动稍后重试", "延长等待后重试"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output contains stale retry guidance %q: %q", unwanted, got)
		}
	}
}

func TestFailureMappings(t *testing.T) {
	cases := map[string]string{
		"network_timeout":              "网络操作超时",
		"network_io_failed":            "网络读写失败",
		"server_busy":                  "认证服务器繁忙",
		"session_in_use":               "账号正在其他位置使用",
		"credential_invalid":           "账号或密码未通过认证",
		"insufficient_funds":           "账号余额不足",
		"account_frozen":               "账号已冻结",
		"binding_ip_mismatch":          "IPv4 地址绑定不匹配",
		"binding_mac_mismatch":         "MAC 地址绑定不匹配",
		"too_many_sessions":            "账号已超过并发会话限制",
		"incompatible_version":         "认证协议版本不兼容",
		"binding_pair_mismatch":        "IPv4 与 MAC 绑定不匹配",
		"dhcp_required":                "服务器要求使用 DHCP 地址",
		"authentication_rejected":      "认证被服务器拒绝",
		"protocol_response_invalid":    "服务器响应无效或不兼容",
		"protocol_contract_violated":   "认证协议违反内部契约",
		"logout_cleanup_failed":        "退出认证清理失败",
		"protocol_run_creation_failed": "无法创建认证协议运行",
	}
	for code, want := range cases {
		if got := failureText(code); got != want {
			t.Errorf("failureText(%q) = %q, want %q", code, got, want)
		}
	}
	if got := failureText("bogus"); got != "未知失败" {
		t.Errorf("unknown failure = %q, want plain fallback", got)
	}
}

func TestRecommendationMappings(t *testing.T) {
	cases := map[string]string{
		"retry_after_standard_delay":                            "将自动稍后重试",
		"retry_after_extended_delay":                            "服务器繁忙，将延长等待后重试",
		"block_until_explicit_restart_or_relevant_input_change": "请检查账号、机构配置和网络后停止并重新启动该 Session",
	}
	for code, want := range cases {
		if got := recommendationText(code); got != want {
			t.Errorf("recommendationText(%q) = %q, want %q", code, got, want)
		}
	}
	if got := recommendationText("bogus"); got != "未知建议" {
		t.Errorf("unknown recommendation = %q, want plain fallback", got)
	}
}

func TestDaemonStatusMappings(t *testing.T) {
	if got := daemonStatusText("running"); got != "运行中" {
		t.Errorf("daemonStatusText(running) = %q, want 运行中", got)
	}
	if got := daemonStatusText("bogus"); got != "未知状态" {
		t.Errorf("unknown daemon status = %q, want 未知状态", got)
	}
}

func TestIPCErrorMappings(t *testing.T) {
	cases := map[string]string{
		"unknown_method":                         "daemon 不支持该操作",
		"malformed_request":                      "daemon 拒绝了格式错误的请求",
		"invalid_argument":                       "请求参数无效",
		"profile_not_found":                      "找不到指定的机构 Profile",
		"protocol_not_found":                     "找不到指定的认证协议",
		"profile_operation_failed":               "机构 Profile 操作失败",
		"session_operation_failed":               "认证 Session 操作失败",
		"configuration_not_found":                "找不到指定的认证配置；请运行 sidraviactl config list 查看可用配置",
		"configuration_conflict":                 "认证配置已存在；请运行 sidraviactl config list 查看现有配置，再运行 sidraviactl help config update 查看更新方法",
		"configuration_operation_failed":         "认证配置操作失败",
		"configuration_auto_login_conflict":      "已有其他配置启用了自动登录",
		"insecure_storage_confirmation_required": "需要确认不安全存储",
	}
	for code, want := range cases {
		if got := ipcErrorText(code); got != want {
			t.Errorf("ipcErrorText(%q) = %q, want %q", code, got, want)
		}
	}
	if got := ipcErrorText("bogus"); got != "daemon 返回了无法识别的错误" {
		t.Errorf("unknown ipc error = %q, want fallback", got)
	}
	if got := ipcErrorText(""); got != "daemon 返回了无法识别的错误" {
		t.Errorf("missing ipc error = %q, want fallback", got)
	}
}

func TestIPCConfigurationErrorsHaveRecoveryCommands(t *testing.T) {
	cases := map[string]string{
		"configuration_not_found": "找不到指定的认证配置；请运行 sidraviactl config list 查看可用配置",
		"configuration_conflict":  "认证配置已存在；请运行 sidraviactl config list 查看现有配置，再运行 sidraviactl help config update 查看更新方法",
	}
	for code, want := range cases {
		if got := ipcErrorText(code); got != want {
			t.Errorf("ipcErrorText(%q) = %q, want %q", code, got, want)
		}
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

func TestRenderSessionDetailSanitizesDynamicValues(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.Ascii)
	defer p.close()
	result := &contract.SessionResult{
		AuthenticationSessionID:  "evil\x1b[2J\nsession",
		InstitutionProfileID:     "p",
		AuthenticationProtocolID: "proto",
		AccountName:              "acc",
		State:                    "authenticated",
		UpdatedAt:                "2026-07-26T10:11:12.123456789+08:00",
	}
	block := renderSessionDetail(p, result)
	if strings.Contains(block, "\x1b[2J") {
		t.Errorf("dynamic ANSI sequence survived into block: %q", block)
	}
	if strings.Contains(block, "\nsession") {
		t.Errorf("dynamic newline injected a new output line: %q", block)
	}
}

func TestDaemonStatusLineContract(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.Ascii)
	defer p.close()
	_ = p.write(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build-1", PID: 42, Status: "running", Mode: "headless"}))
	want := "守护进程：运行中（running） | 版本：1.0.0 | 构建：build-1 | PID：42 | 模式：headless\n" +
		"日志：portable 模式为程序目录下 logs/sidraviad.log；安装版为当前用户缓存目录下 Sidravia/logs/sidraviad.log。\n"
	if buf.String() != want {
		t.Errorf("daemon status = %q, want %q", buf.String(), want)
	}
}

func TestDaemonStatusIncludesLogLocationGuidance(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.Ascii)
	defer p.close()
	_ = p.write(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build-1", PID: 42, Status: "running", Mode: "headless"}))
	wantStatus := "守护进程：运行中（running） | 版本：1.0.0 | 构建：build-1 | PID：42 | 模式：headless\n"
	wantLog := "日志：portable 模式为程序目录下 logs/sidraviad.log；安装版为当前用户缓存目录下 Sidravia/logs/sidraviad.log。\n"
	if got := buf.String(); got != wantStatus+wantLog {
		t.Fatalf("daemon status guidance = %q, want %q", got, wantStatus+wantLog)
	}
	for _, private := range []string{"ws://", "runtime.json", "AppData", "/home/", "endpoint"} {
		if strings.Contains(buf.String(), private) {
			t.Errorf("daemon status leaked private implementation text %q: %q", private, buf.String())
		}
	}
}

func TestDaemonStatusUnknownCodeContract(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.Ascii)
	defer p.close()
	_ = p.write(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1", BuildID: "b", PID: 1, Status: "weird"}))
	if !strings.Contains(buf.String(), "未知状态（weird）") {
		t.Errorf("unknown daemon status = %q, want fallback with sanitized code", buf.String())
	}
}

func TestRenderedBlockWriterFailurePreservesCause(t *testing.T) {
	cause := errors.New("injected writer failure")
	err := writeSessionResult(zeroWriter{err: cause}, minimalSessionResult("authenticated"))
	if !errors.Is(err, cause) {
		t.Errorf("writeSessionResult did not preserve writer cause: %v", err)
	}
}

func TestPasswordPromptIsChinese(t *testing.T) {
	var output bytes.Buffer
	_, err := readHiddenPassword(
		strings.NewReader("secret\n"),
		&output,
		0x0004,
		func() (uint32, error) { return 0x0017, nil },
		func(uint32) error { return nil },
	)
	if err != nil {
		t.Fatalf("readHiddenPassword = %v", err)
	}
	if !strings.HasPrefix(output.String(), "密码： ") {
		t.Errorf("prompt = %q, want Chinese prompt with full-width colon and trailing ASCII space", output.String())
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

func TestSessionListRenderingUsesANSIStylesUnderForcedProfile(t *testing.T) {
	result := &contract.SessionListResult{Sessions: []contract.SessionResult{
		minimalSessionResult("authenticated"),
		minimalSessionResult("blocked_by_error"),
		minimalSessionResult("suspended"),
	}}
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.ANSI)
	defer p.close()
	if err := p.write(renderSessionList(p, result)); err != nil {
		t.Fatalf("write = %v", err)
	}
	out := buf.String()
	if !strings.ContainsRune(out, '\x1b') {
		t.Errorf("ANSI session list output contains no ESC: %q", out)
	}
	if !strings.Contains(out, "会话（") || !strings.Contains(out, "已认证") || !strings.Contains(out, "因错误阻塞") {
		t.Errorf("ANSI session list output missing heading or mapped state text: %q", out)
	}
}

func TestSessionListDiagnosticGuidanceSanitizesSessionIDAndSkipsUnknownState(t *testing.T) {
	result := contract.SessionListResult{Sessions: []contract.SessionResult{
		{
			AuthenticationSessionID: "blocked\x1b[2J\ninjected",
			InstitutionProfileID:    "jlu",
			InstitutionDisplayName:  "吉林大学",
			AccountName:             "alice",
			State:                   "blocked_by_error",
			UpdatedAt:               "2026-08-03T10:00:00+08:00",
		},
		{
			AuthenticationSessionID: "unknown-session",
			InstitutionProfileID:    "jlu",
			InstitutionDisplayName:  "吉林大学",
			AccountName:             "bob",
			State:                   "unknown_state",
			UpdatedAt:               "2026-08-03T10:01:00+08:00",
		},
	}}
	var output bytes.Buffer
	if err := writeSessionList(&output, result); err != nil {
		t.Fatalf("writeSessionList = %v", err)
	}
	got := output.String()
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("diagnostic guidance contains ESC: %q", got)
	}
	if strings.Count(got, "\n") != 4 {
		t.Errorf("diagnostic guidance allowed an extra dynamic line: %q", got)
	}
	if !strings.Contains(got, "  查看详情：sidraviactl auth status blocked�[2J�injected\n") {
		t.Errorf("blocked Session guidance missing or unsanitized: %q", got)
	}
	if strings.Contains(got, "sidraviactl auth status unknown-session") {
		t.Errorf("unknown state received diagnostic guidance: %q", got)
	}
}

func TestProfileListRenderingUsesANSIStylesUnderForcedProfile(t *testing.T) {
	result := &contract.ProfileListResult{Profiles: []contract.ProfileSummaryResult{{
		InstitutionProfileID:     "jlu",
		DisplayName:              "吉林大学",
		AuthenticationProtocolID: "drcom-5.2.0-d",
	}}}
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.ANSI)
	defer p.close()
	if err := p.write(renderProfileList(p, result)); err != nil {
		t.Fatalf("write = %v", err)
	}
	out := buf.String()
	if !strings.ContainsRune(out, '\x1b') {
		t.Errorf("ANSI profile list output contains no ESC: %q", out)
	}
	if !strings.Contains(out, "机构 Profile") || !strings.Contains(out, "吉林大学") {
		t.Errorf("ANSI profile list output missing content: %q", out)
	}
}

func TestRedirectedListContainsNoESC(t *testing.T) {
	t.Run("sessions", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeSessionList(&buf, contract.SessionListResult{Sessions: []contract.SessionResult{minimalSessionResult("authenticated")}}); err != nil {
			t.Fatalf("writeSessionList = %v", err)
		}
		if strings.ContainsRune(buf.String(), '\x1b') {
			t.Errorf("redirected session list contains ESC: %q", buf.String())
		}
	})
	t.Run("profiles", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeProfileList(&buf, contract.ProfileListResult{Profiles: []contract.ProfileSummaryResult{{
			InstitutionProfileID:     "jlu",
			DisplayName:              "吉林大学",
			AuthenticationProtocolID: "drcom-5.2.0-d",
		}}}); err != nil {
			t.Fatalf("writeProfileList = %v", err)
		}
		if strings.ContainsRune(buf.String(), '\x1b') {
			t.Errorf("redirected profile list contains ESC: %q", buf.String())
		}
	})
}

func TestUnknownCodesRenderedOnce(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.Ascii)
	defer p.close()
	result := &contract.SessionResult{
		AuthenticationSessionID:   "s",
		InstitutionProfileID:      "p",
		AuthenticationProtocolID:  "proto",
		AccountName:               "acc",
		State:                     "bogus-state",
		UpdatedAt:                 "2026-07-26T10:11:12.123456789+08:00",
		StateReason:               &contract.SessionStateReason{Code: "bogus-reason"},
		LastAuthenticationFailure: &contract.SessionAuthenticationFailure{Code: "bogus-failure", HandlingRecommendation: "bogus-recommendation"},
	}
	if err := p.write(renderSessionDetail(p, result)); err != nil {
		t.Fatalf("write = %v", err)
	}
	out := buf.String()
	cases := []struct {
		label string
		code  string
	}{
		{"未知状态", "bogus-state"},
		{"未知原因", "bogus-reason"},
		{"未知失败", "bogus-failure"},
		{"未知建议", "bogus-recommendation"},
	}
	for _, c := range cases {
		if got := strings.Count(out, c.code); got != 1 {
			t.Errorf("unknown code %q appears %d times, want 1: %q", c.code, got, out)
		}
		if !strings.Contains(out, c.label+"（"+c.code+"）") {
			t.Errorf("unknown code %q not rendered once as %q: %q", c.code, c.label+"（"+c.code+"）", out)
		}
	}
}

func TestSessionDetailNetworkLineSeparator(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPresentation(&buf, termenv.Ascii)
	defer p.close()
	result := &contract.SessionResult{
		AuthenticationSessionID:  "s",
		InstitutionProfileID:     "p",
		AuthenticationProtocolID: "proto",
		AccountName:              "acc",
		State:                    "authenticated",
		UpdatedAt:                "2026-07-26T10:11:12.123456789+08:00",
		SelectedNetworkBinding: &contract.SessionNetworkBinding{
			DisplayName:      "Campus Ethernet",
			InterfaceID:      "if-7",
			LocalIPv4Address: "192.0.2.25",
		},
	}
	if err := p.write(renderSessionDetail(p, result)); err != nil {
		t.Fatalf("write = %v", err)
	}
	want := "网络：Campus Ethernet — 192.0.2.25\n"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("network line = %q, want %q", buf.String(), want)
	}
}

func TestWriteErrorSanitizesAndHidesCause(t *testing.T) {
	cause := errors.New("injected underlying cause")
	safe := wrapSafeOperation("连接 sidraviad", cause)
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

func TestDaemonLifecycleRenderers(t *testing.T) {
	if got := renderDaemonStarted(); got != "守护进程：已启动（started）\n" {
		t.Errorf("started = %q", got)
	}
	if got := renderDaemonStopped(); got != "守护进程：已停止（stopped）\n" {
		t.Errorf("stopped = %q", got)
	}
	if got := renderDaemonRestarted(); got != "守护进程：已重启（restarted）\n" {
		t.Errorf("restarted = %q", got)
	}
}

func TestActionableSessionErrorGuidanceAndEmptyStates(t *testing.T) {
	if got := ipcErrorText("session_not_found"); got != "找不到指定的认证 Session；请运行 sidraviactl auth list 查看可用 Session" {
		t.Fatalf("session_not_found guidance = %q", got)
	}

	var sessions bytes.Buffer
	p := newTestPresentation(&sessions, termenv.Ascii)
	if err := p.complete(renderSessionList(p, &contract.SessionListResult{})); err != nil {
		t.Fatal(err)
	}
	if got := sessions.String(); got != "没有 Session。\n下一步：运行 sidraviactl help auth start 开始认证。\n" {
		t.Fatalf("empty Session list = %q", got)
	}
}

func TestConfigurationPresentationProtectedUnprotectedEmptyAndSanitized(t *testing.T) {
	base := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus\x1b[2J\ninjected", DisplayName: "校园网",
		InstitutionProfileID: "jlu", InstitutionDisplayName: "吉林大学",
		AuthenticationProtocolID: "drcom", Username: "user\nmarker",
		CredentialStored: true, StorageProtection: "protected",
	}
	protected := renderConfiguration(base)
	if !strings.Contains(protected, "已保护（protected）") || strings.ContainsAny(protected, "\x1b") ||
		strings.Contains(protected, "\ninjected") || strings.Contains(protected, "\nmarker") ||
		strings.Contains(strings.ToLower(protected), "password") {
		t.Fatalf("protected configuration output unsafe: %q", protected)
	}
	base.StorageProtection = "unprotected"
	if got := renderConfiguration(base); !strings.Contains(got, "未保护（unprotected）") {
		t.Fatalf("unprotected output = %q", got)
	}
	var empty bytes.Buffer
	if err := writeConfigurationList(&empty, contract.ConfigurationListResult{StorageProtection: "protected", Configurations: []contract.ConfigurationResult{}}); err != nil {
		t.Fatal(err)
	}
	if empty.String() != "尚未保存认证配置。\n下一步：运行 sidraviactl config create 创建认证配置。\n" {
		t.Fatalf("empty list = %q", empty.String())
	}
}

func TestConfigurationOmitsUnsetOptionalName(t *testing.T) {
	withName := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", DisplayName: "校园\x1b[2J\n配置",
		InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
	}
	if got := renderConfiguration(withName); !strings.Contains(got, "名称：校园�[2J�配置\n") {
		t.Fatalf("present optional name was not sanitized and rendered: %q", got)
	}
	withoutName := withName
	withoutName.DisplayName = ""
	got := renderConfiguration(withoutName)
	if strings.Contains(got, "名称：") {
		t.Fatalf("unset optional name rendered an empty row: %q", got)
	}
	wantOrder := []string{"配置：campus\n", "机构：JLU\n", "协议：drcom\n", "可用状态：可用\n", "账号：user\n", "凭据：已保存\n", "存储保护：已保护（protected）\n", "自动登录：未启用\n", "自动重连：未启用\n", "网络绑定：自动选择可用网卡\n"}
	if got != strings.Join(wantOrder, "") {
		t.Fatalf("configuration rows changed when optional name was omitted: %q", got)
	}
}

func TestConfigurationForcedColorDoesNotStyleDynamicValues(t *testing.T) {
	result := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", InstitutionProfileID: "jlu",
		AuthenticationProtocolID: "drcom", Username: "user",
		CredentialStored: true, StorageProtection: "protected",
	}
	block := renderConfiguration(result)
	var output bytes.Buffer
	p := newTestPresentation(&output, termenv.ANSI)
	if err := p.complete(block); err != nil {
		t.Fatal(err)
	}
	if output.String() != block || strings.ContainsRune(output.String(), '\x1b') {
		t.Fatalf("configuration block unexpectedly styled: %q", output.String())
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
