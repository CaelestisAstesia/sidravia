package terminal

import (
	"fmt"
	"strconv"
	"strings"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
)

// RenderDaemonStatus renders a daemon.status result as one line. Field labels
// are styled; the status value is plain because daemon status codes are not
// Session states.
func RenderDaemonStatus(p *Presentation, result *contract.StatusResult) string {
	var b strings.Builder
	b.WriteString(p.Label("守护进程："))
	b.WriteString(daemonStatusText(result.Status))
	b.WriteString("（")
	b.WriteString(Sanitize(result.Status))
	b.WriteString("） | ")
	b.WriteString(p.Label("版本："))
	b.WriteString(Sanitize(result.ProductVersion))
	b.WriteString(" | ")
	b.WriteString(p.Label("构建："))
	b.WriteString(Sanitize(result.BuildID))
	b.WriteString(" | ")
	b.WriteString(p.Label("PID："))
	b.WriteString(strconv.Itoa(result.PID))
	b.WriteString(" | ")
	b.WriteString(p.Label("模式："))
	b.WriteString(Sanitize(result.Mode))
	b.WriteString("\n")
	if result.Mode == string(launchcontract.ModeSystemd) {
		b.WriteString("日志：运行 journalctl --user -u sidravia.service 查看后台日志。\n")
	} else {
		b.WriteString("日志：portable 模式为程序目录下 logs/sidraviad.log；安装版为当前用户缓存目录下 Sidravia/logs/sidraviad.log。\n")
	}
	return b.String()
}

// jluProfileID is the stable machine Profile ID and JSON value for the built-in
// Jilin University profile. It stays lowercase in every machine contract.
const jluProfileID = "jlu"

// jluHumanLabel is the fixed human identifier for the built-in JLU profile. The
// machine ID remains the lowercase jluProfileID; only ordinary human output
// uses the capitalized institution abbreviation. It is static trusted text, so
// it never passes through sanitization.
const jluHumanLabel = "吉林大学（JLU）"

// sessionInstitutionText renders the institution line for a Session: the built-in
// JLU profile renders as its fixed human identifier; other profiles render the
// display name followed by the profile id in full-width parentheses when a
// display name is present, otherwise just the profile id. All non-JLU dynamic
// values are sanitized. The detail and list renderers share it.
func sessionInstitutionText(session contract.SessionResult) string {
	if session.InstitutionProfileID == jluProfileID {
		return jluHumanLabel
	}
	if session.InstitutionDisplayName == "" {
		return Sanitize(session.InstitutionProfileID)
	}
	return Sanitize(session.InstitutionDisplayName) + "（" + Sanitize(session.InstitutionProfileID) + "）"
}

// InstitutionDisplayName returns the human display name for an institution
// profile summary. The built-in JLU profile renders as its fixed human
// identifier; other profiles use their safe display name unchanged.
func InstitutionDisplayName(profileID, displayName string) string {
	if profileID == jluProfileID {
		return jluHumanLabel
	}
	return Sanitize(displayName)
}

// NetworkBindingText renders the selected network binding for humans: when a
// friendly display name is present it shows "<friendly name> — <IPv4>",
// otherwise only "<IPv4>". The machine InterfaceID stays in the DTO for machine
// consumers but never appears in ordinary output.
func NetworkBindingText(binding *contract.SessionNetworkBinding) string {
	ipv4 := Sanitize(binding.LocalIPv4Address)
	name := Sanitize(binding.DisplayName)
	if name == "" {
		return ipv4
	}
	return name + " — " + ipv4
}

// RenderSessionDetail renders a complete Session detail block. Optional lines
// are omitted under the same conditions as before. Only trusted mapped state
// text is styled; all dynamic values are sanitized and descriptions are never
// printed.
func RenderSessionDetail(p *Presentation, result *contract.SessionResult) string {
	var b strings.Builder
	b.WriteString(p.Label("会话："))
	b.WriteString(Sanitize(result.AuthenticationSessionID))
	b.WriteString("\n")
	if result.DisplayName != "" {
		b.WriteString(p.Label("名称："))
		b.WriteString(Sanitize(result.DisplayName))
		b.WriteString("\n")
	}

	b.WriteString(p.Label("状态："))
	b.WriteString(p.State(SessionStateText(result.State), result.State))
	b.WriteString("（")
	b.WriteString(Sanitize(result.State))
	b.WriteString("）\n")

	b.WriteString(p.Label("机构："))
	b.WriteString(sessionInstitutionText(*result))
	b.WriteString("\n")

	b.WriteString(p.Label("协议："))
	b.WriteString(Sanitize(result.AuthenticationProtocolID))
	b.WriteString("\n")

	b.WriteString(p.Label("账号："))
	b.WriteString(Sanitize(result.AccountName))
	b.WriteString("\n")

	if result.StateReason != nil {
		b.WriteString(p.Label("原因："))
		b.WriteString(SessionStateReasonText(result.StateReason.Code))
		b.WriteString("（")
		b.WriteString(Sanitize(result.StateReason.Code))
		b.WriteString("）\n")
	}

	if result.SelectedNetworkBinding != nil {
		b.WriteString(p.Label("网络："))
		b.WriteString(NetworkBindingText(result.SelectedNetworkBinding))
		b.WriteString("\n")
	}

	if result.AuthenticationEstablishedAt != nil {
		b.WriteString(p.Label("认证时间："))
		b.WriteString(Sanitize(*result.AuthenticationEstablishedAt))
		b.WriteString("\n")
	}

	if result.NextRetryAt != nil {
		b.WriteString(p.Label("下次重试："))
		b.WriteString(Sanitize(*result.NextRetryAt))
		b.WriteString("\n")
	}

	if result.LastAuthenticationFailure != nil {
		failure := result.LastAuthenticationFailure
		b.WriteString(p.Label("最近失败："))
		b.WriteString(FailureText(failure.Code))
		b.WriteString("（")
		b.WriteString(Sanitize(failure.Code))
		b.WriteString("）\n")

		b.WriteString(p.Label("处理建议："))
		b.WriteString(RecommendationText(failure.HandlingRecommendation))
		b.WriteString("（")
		b.WriteString(Sanitize(failure.HandlingRecommendation))
		b.WriteString("）\n")
	}

	b.WriteString(p.Label("更新时间："))
	b.WriteString(Sanitize(result.UpdatedAt))
	b.WriteString("\n")

	return b.String()
}

// RenderSessionList renders a complete Session list block. The heading and
// fixed labels are styled with bold cyan; mapped Session state text uses the
// same green/red/yellow/plain policy as the detail renderer; every dynamic
// value is sanitized. An empty list renders the existing plain Chinese text.
func RenderSessionList(p *Presentation, result *contract.SessionListResult) string {
	var b strings.Builder
	if len(result.Sessions) == 0 {
		b.WriteString("没有 Session。\n下一步：运行 sidraviactl help auth start 开始认证。\n")
		return b.String()
	}
	b.WriteString(p.Label(fmt.Sprintf("会话（%d）：", len(result.Sessions))))
	b.WriteString("\n")
	for _, session := range result.Sessions {
		b.WriteString("- ")
		b.WriteString(Sanitize(session.AuthenticationSessionID))
		if session.DisplayName != "" {
			b.WriteString(" | ")
			b.WriteString(p.Label("名称："))
			b.WriteString(Sanitize(session.DisplayName))
		}
		b.WriteString(" | ")
		b.WriteString(p.Label("状态："))
		b.WriteString(p.State(SessionStateText(session.State), session.State))
		b.WriteString("（")
		b.WriteString(Sanitize(session.State))
		b.WriteString("） | ")
		b.WriteString(p.Label("机构："))
		b.WriteString(sessionInstitutionText(session))
		b.WriteString(" | ")
		b.WriteString(p.Label("账号："))
		b.WriteString(Sanitize(session.AccountName))
		b.WriteString(" | ")
		b.WriteString(p.Label("更新时间："))
		b.WriteString(Sanitize(session.UpdatedAt))
		b.WriteString("\n")
		for _, id := range result.CleanupRequiredSessionIDs {
			if id == session.AuthenticationSessionID {
				b.WriteString("  需要清理：运行 sidraviactl auth remove 释放此 Session。\n")
			}
		}
		if session.State == "blocked_by_error" || session.State == "waiting_before_retry" {
			b.WriteString("  查看详情：sidraviactl auth status ")
			b.WriteString(Sanitize(session.AuthenticationSessionID))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// RenderDaemonStarted renders the fixed daemon started confirmation line.
func RenderDaemonStarted() string {
	return "守护进程：已启动（started）\n"
}

func RenderDaemonAlreadyRunning() string {
	return "守护进程：已经运行（already_running）\n"
}

// RenderDaemonStopped renders the fixed daemon stopped confirmation line.
func RenderDaemonStopped() string {
	return "守护进程：已停止（stopped）\n"
}

func RenderDaemonAlreadyStopped() string {
	return "守护进程：已经停止（already_stopped）\n"
}

// RenderDaemonRestarted renders the fixed daemon restarted confirmation line.
func RenderDaemonRestarted() string {
	return "守护进程：已重启（restarted）\n"
}

func RenderDaemonStartedFromStopped() string {
	return "守护进程：原先未运行，现已启动（started_from_stopped）\n"
}

func RenderSessionStartResult(p *Presentation, result *contract.SessionStartResult) string {
	var b strings.Builder
	switch result.Outcome {
	case "created":
		b.WriteString("认证 Session：已创建。\n")
	case "already_running":
		b.WriteString("认证 Session：已经运行。\n")
	case "resumed":
		b.WriteString("认证 Session：已恢复运行。\n")
	}
	b.WriteString("以下是当前 Snapshot；认证可能仍在 daemon 中继续。\n")
	b.WriteString(RenderSessionDetail(p, &result.Session))
	b.WriteString("查看状态：sidraviactl auth status ")
	b.WriteString(Sanitize(result.Session.AuthenticationSessionID))
	b.WriteString("\n")
	return b.String()
}

func RenderSessionRemoved(result *contract.SessionRemoveResult) string {
	return "Session 已删除：" + Sanitize(result.SessionID) + "\n"
}
