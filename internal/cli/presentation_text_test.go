package cli

import (
	"bytes"
	"testing"

	"github.com/muesli/termenv"

	"sidravia/internal/ipc/contract"
)

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

func TestFailureMappings(t *testing.T) {
	cases := map[string]string{
		"quota_exceeded":               "账号时间或流量配额已耗尽",
		"account_suspended":            "服务器报告账号已暂停使用",
		"account_expired":              "账号有效期已结束",
		"too_many_addresses":           "服务器拒绝了上报的 IPv4 地址数量",
		"server_session_reset":         "服务器已重置连接",
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
