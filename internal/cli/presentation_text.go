package cli

import (
	"sidravia/internal/ipc/contract"
)

// sessionStateText maps a Session state code to Simplified Chinese. Unknown
// nonempty codes use the safe plain fallback; the renderer appends the
// sanitized machine code in full-width parentheses exactly once.
func sessionStateText(code string) string {
	switch code {
	case "suspended":
		return "已暂停"
	case "waiting_for_network":
		return "等待可用网络"
	case "authenticating":
		return "正在认证"
	case "authenticated":
		return "已认证"
	case "waiting_before_retry":
		return "等待重试"
	case "blocked_by_error":
		return "因错误阻塞"
	case "stopping":
		return "正在停止"
	default:
		return "未知状态"
	}
}

// sessionStateReasonText maps a Session state reason code to Simplified Chinese.
func sessionStateReasonText(code string) string {
	switch code {
	case "network_binding_unavailable":
		return "指定网卡或 IPv4 地址不可用"
	case "network_unavailable":
		return "没有可用网络"
	case "runtime_definition_unavailable":
		return "认证运行配置不可用"
	case "protocol_run_creation_failed":
		return "无法创建认证协议运行"
	case "protocol_run_failed":
		return "认证协议运行失败"
	case "protocol_contract_violated":
		return "认证协议违反内部契约"
	case "automatic_reconnect_disabled":
		return "自动重连已关闭"
	case "protocol_recovery_exhausted":
		return "已尝试 3 次自动恢复，请手动重新连接"
	default:
		return "未知原因"
	}
}

// failureText maps a D520/Session authentication failure code to Simplified Chinese.
func failureText(code string) string {
	switch code {
	case "quota_exceeded":
		return "账号时间或流量配额已耗尽"
	case "account_suspended":
		return "服务器报告账号已暂停使用"
	case "account_expired":
		return "账号有效期已结束"
	case "too_many_addresses":
		return "服务器拒绝了上报的 IPv4 地址数量"
	case "server_session_reset":
		return "服务器已重置连接"
	case "server_session_ended":
		return "服务器报告连接已离线"
	case "negotiation_unsupported":
		return "服务器要求的协议能力尚不支持"
	case "network_timeout":
		return "网络操作超时"
	case "network_io_failed":
		return "网络读写失败"
	case "server_busy":
		return "认证服务器繁忙"
	case "session_in_use":
		return "账号正在其他位置使用"
	case "credential_invalid":
		return "账号或密码未通过认证"
	case "insufficient_funds":
		return "账号余额不足"
	case "account_frozen":
		return "账号已冻结"
	case "binding_ip_mismatch":
		return "IPv4 地址绑定不匹配"
	case "binding_mac_mismatch":
		return "MAC 地址绑定不匹配"
	case "too_many_sessions":
		return "账号已超过并发会话限制"
	case "incompatible_version":
		return "认证协议版本不兼容"
	case "binding_pair_mismatch":
		return "IPv4 与 MAC 绑定不匹配"
	case "dhcp_required":
		return "服务器要求使用 DHCP 地址"
	case "authentication_rejected":
		return "认证被服务器拒绝"
	case "protocol_response_invalid":
		return "服务器响应无效或不兼容"
	case "protocol_contract_violated":
		return "认证协议违反内部契约"
	case "logout_cleanup_failed":
		return "退出认证清理失败"
	case "protocol_run_creation_failed":
		return "无法创建认证协议运行"
	default:
		return "未知失败"
	}
}

// recommendationText maps a failure handling recommendation code to Simplified Chinese.
func recommendationText(code string) string {
	switch code {
	case "retry_after_standard_delay":
		return "将自动稍后重试"
	case "retry_after_extended_delay":
		return "服务器繁忙，将延长等待后重试"
	case "retry_after_bounded_recovery":
		return "将有限次数重新认证，连续失败后停止自动恢复"
	case "block_until_explicit_restart_or_relevant_input_change":
		return "请检查账号、机构配置和网络后停止并重新启动该 Session"
	default:
		return "未知建议"
	}
}

// daemonStatusText maps a daemon status code to Simplified Chinese. Unknown
// nonempty codes use the safe fallback.
func daemonStatusText(code string) string {
	if code == "running" {
		return "运行中"
	}
	return "未知状态"
}

// ipcErrorText maps an IPC error code to fixed Simplified Chinese guidance.
// Unknown or missing codes use the safe generic message.
func ipcErrorText(code string) string {
	if code == contract.ErrorCodeConfigurationSessionInvalidationFailed {
		return "配置已提交，旧会话清理未完成；请移除旧会话后重试连接"
	}
	switch code {
	case contract.ErrorCodeStateSnapshotTooLarge:
		return "daemon 状态快照过大；请更新 Sidravia 后重试"
	case contract.ErrorCodeStateSnapshotUnavailable:
		return "daemon 当前无法提供状态快照；请稍后重试"
	case "unknown_method":
		return "daemon 不支持该操作"
	case "malformed_request":
		return "daemon 拒绝了格式错误的请求"
	case "invalid_argument":
		return "请求参数无效"
	case "profile_not_found":
		return "找不到指定的机构 Profile"
	case "protocol_not_found":
		return "找不到指定的认证协议"
	case "profile_operation_failed":
		return "机构 Profile 操作失败"
	case "session_operation_failed":
		return "认证 Session 操作失败"
	case "session_not_found":
		return "找不到指定的认证 Session；请运行 sidraviactl auth list 查看可用 Session"
	case "session_active_conflict":
		return "已有认证 Session 正在运行；请先停止或删除现有活动 Session"
	case "session_state_conflict":
		return "认证 Session 当前状态不允许该操作；请运行 sidraviactl auth status 查看状态"
	case "configuration_not_found":
		return "找不到指定的认证配置；请运行 sidraviactl config list 查看可用配置"
	case "configuration_conflict":
		return "认证配置已存在；请运行 sidraviactl config list 查看现有配置，再运行 sidraviactl help config update 查看更新方法"
	case "configuration_operation_failed":
		return "认证配置操作失败"
	case "configuration_auto_login_conflict":
		return "已有其他配置启用了自动登录"
	case "insecure_storage_confirmation_required":
		return "需要确认不安全存储"
	default:
		return "daemon 返回了无法识别的错误"
	}
}
