package cli

import (
	"bytes"
	"github.com/muesli/termenv"
	"sidravia/internal/ipc/contract"
	"strings"
	"testing"
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
	for _, tc := range []struct{ mode, wantLog string }{
		{"desktop", "日志：portable 模式为程序目录下 logs/sidraviad.log；安装版为当前用户缓存目录下 Sidravia/logs/sidraviad.log。\n"},
		{"systemd", "日志：运行 journalctl --user -u sidravia.service 查看后台日志。\n"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			var buf bytes.Buffer
			p := newTestPresentation(&buf, termenv.Ascii)
			defer p.close()
			_ = p.write(renderDaemonStatus(p, &contract.StatusResult{ProductVersion: "1.0.0", BuildID: "build-1", PID: 42, Status: "running", Mode: tc.mode}))
			wantStatus := "守护进程：运行中（running） | 版本：1.0.0 | 构建：build-1 | PID：42 | 模式：" + tc.mode + "\n"
			if got := buf.String(); got != wantStatus+tc.wantLog {
				t.Fatalf("daemon status guidance = %q, want %q", got, wantStatus+tc.wantLog)
			}
			for _, private := range []string{"ws://", "runtime.json", "AppData", "/home/", "endpoint"} {
				if strings.Contains(buf.String(), private) {
					t.Errorf("daemon status leaked private implementation text %q: %q", private, buf.String())
				}
			}
		})
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

func TestProtocolRecoveryPresentation(t *testing.T) {
	if got := sessionStateReasonText("protocol_recovery_exhausted"); got != "已尝试 3 次自动恢复，请手动重新连接" {
		t.Fatalf("reason=%q", got)
	}
	if got := recommendationText("retry_after_bounded_recovery"); got != "将有限次数重新认证，连续失败后停止自动恢复" {
		t.Fatalf("recommendation=%q", got)
	}
}
