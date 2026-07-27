package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/muesli/termenv"

	"sidravia/internal/ipc/contract"
)

// presentation.go is the sole CLI presentation boundary and the only production
// file that imports termenv. It resolves terminal capability, owns Windows
// virtual-terminal enable/restore, styles trusted static labels and mapped
// state text, sanitizes dynamic values, and renders complete blocks before
// they are written.
//
// Business operations continue to return errors and DTOs. Presentation never
// owns daemon discovery, IPC calls, password contents, Session state, retry
// decisions, persistence or protocol behavior. No termenv type crosses out of
// internal/cli.

const replacementRune = "�"

// ANSI color codes used exclusively for trusted static labels and mapped state
// text. Dynamic values never reach the color parser.
const (
	cyanColor   = "6"
	greenColor  = "2"
	redColor    = "1"
	yellowColor = "3"
)

// isControlRune reports whether r is a C0 control character, DEL, or a C1
// control character. These are the characters that can inject terminal control
// sequences or split output lines.
func isControlRune(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// sanitizeDynamicText replaces every C0/C1 control character and DEL in s with
// the replacement rune. Ordinary Unicode, spaces, punctuation, IDs, timestamps
// and Chinese text are preserved. Every value originating outside fixed CLI
// literals passes through this before output.
func sanitizeDynamicText(s string) string {
	if !strings.ContainsFunc(s, isControlRune) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControlRune(r) {
			b.WriteString(replacementRune)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// presentation bundles the resolved terminal capability, the writer and the
// Windows virtual-terminal restore function.
type presentation struct {
	output  *termenv.Output
	profile termenv.Profile
	restore func() error
}

// resolveColorProfile is the terminal capability gate. The writer's actual
// ColorProfile is the first gate: when it is Ascii (a redirected or
// non-interactive writer) output stays plain regardless of CLICOLOR_FORCE.
// Only a color-capable real terminal is refined by EnvColorProfile, which
// honors NO_COLOR.
func resolveColorProfile(capability, envProfile termenv.Profile) termenv.Profile {
	if capability == termenv.Ascii {
		return termenv.Ascii
	}
	return envProfile
}

// resolveVTEnable decides the final profile and restore function after a
// Windows virtual-terminal enable attempt. An enable failure forces plain text
// and installs a safe no-op restore so no ANSI escape is emitted on that path.
// A successful enable keeps the resolved profile and the real restore function.
// The helper is pure: tests cover the decision without replacing the termenv
// function or adding package-global injection state.
func resolveVTEnable(profile termenv.Profile, restore func() error, enableErr error) (termenv.Profile, func() error) {
	if enableErr != nil {
		return termenv.Ascii, func() error { return nil }
	}
	if restore == nil {
		return profile, func() error { return nil }
	}
	return profile, restore
}

// newPresentation resolves the terminal capability for w. It never forces
// ANSI in production: a redirected or non-interactive writer stays plain.
// Virtual-terminal enablement is optional; when it fails the command continues
// with plain text and a safe no-op restore, so no ANSI escape is emitted.
func newPresentation(w io.Writer) *presentation {
	out := termenv.NewOutput(w)
	profile := resolveColorProfile(out.ColorProfile(), out.EnvColorProfile())
	restore, enableErr := termenv.EnableVirtualTerminalProcessing(out)
	profile, restore = resolveVTEnable(profile, restore, enableErr)
	return &presentation{output: out, profile: profile, restore: restore}
}

// newTestPresentation forces a specific profile for tests. It never queries the
// developer terminal, never enables virtual-terminal processing, and never
// mutates package-global termenv state.
func newTestPresentation(w io.Writer, profile termenv.Profile) *presentation {
	out := termenv.NewOutput(w, termenv.WithProfile(profile), termenv.WithTTY(false))
	return &presentation{output: out, profile: profile, restore: func() error { return nil }}
}

// close restores Windows virtual-terminal state if it was enabled. It must run
// after command output.
func (p *presentation) close() {
	_ = p.restore()
}

// write writes a fully rendered block to the writer. It rejects short writes.
func (p *presentation) write(block string) error {
	written, err := io.WriteString(p.output, block)
	if err != nil {
		return err
	}
	if written != len(block) {
		return io.ErrShortWrite
	}
	return nil
}

// complete writes a fully rendered block and then restores Windows
// virtual-terminal state. The restore always runs after the attempted write,
// even when the write failed. It returns the write error, the restore error,
// or both joined so errors.Is observes each cause.
func (p *presentation) complete(block string) error {
	writeErr := p.write(block)
	restoreErr := p.restore()
	return errors.Join(writeErr, restoreErr)
}

// label styles a trusted static field label or section heading with bold cyan.
// The argument must be a fixed CLI literal, never a dynamic value.
func (p *presentation) label(text string) string {
	return p.profile.String(text).Foreground(p.profile.Color(cyanColor)).Bold().String()
}

// state styles mapped Session-state text according to the stable state code.
// The code only selects the style; only the trusted mapped text is styled.
// suspended and unknown states render plain.
func (p *presentation) state(mapped, code string) string {
	styled := p.profile.String(mapped)
	switch code {
	case "authenticated":
		styled = styled.Foreground(p.profile.Color(greenColor)).Bold()
	case "blocked_by_error":
		styled = styled.Foreground(p.profile.Color(redColor)).Bold()
	case "waiting_for_network", "authenticating", "waiting_before_retry", "stopping":
		styled = styled.Foreground(p.profile.Color(yellowColor))
	default:
		return mapped
	}
	return styled.String()
}

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
	default:
		return "未知原因"
	}
}

// failureText maps a D520/Session authentication failure code to Simplified Chinese.
func failureText(code string) string {
	switch code {
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
	switch code {
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
	default:
		return "daemon 返回了无法识别的错误"
	}
}

// renderDaemonStatus renders a daemon.status result as one line. Field labels
// are styled; the status value is plain because daemon status codes are not
// Session states.
func renderDaemonStatus(p *presentation, result *contract.StatusResult) string {
	var b strings.Builder
	b.WriteString(p.label("守护进程："))
	b.WriteString(daemonStatusText(result.Status))
	b.WriteString("（")
	b.WriteString(sanitizeDynamicText(result.Status))
	b.WriteString("） | ")
	b.WriteString(p.label("版本："))
	b.WriteString(sanitizeDynamicText(result.ProductVersion))
	b.WriteString(" | ")
	b.WriteString(p.label("构建："))
	b.WriteString(sanitizeDynamicText(result.BuildID))
	b.WriteString(" | ")
	b.WriteString(p.label("PID："))
	b.WriteString(strconv.Itoa(result.PID))
	b.WriteString("\n")
	return b.String()
}

// sessionInstitutionText renders the institution line for a Session: the
// display name followed by the profile id in full-width parentheses when a
// display name is present, otherwise just the profile id. All values are
// sanitized. The detail and list renderers share it.
func sessionInstitutionText(session contract.SessionResult) string {
	if session.InstitutionDisplayName == "" {
		return sanitizeDynamicText(session.InstitutionProfileID)
	}
	return sanitizeDynamicText(session.InstitutionDisplayName) + "（" + sanitizeDynamicText(session.InstitutionProfileID) + "）"
}

// renderSessionDetail renders a complete Session detail block. Optional lines
// are omitted under the same conditions as before. Only trusted mapped state
// text is styled; all dynamic values are sanitized and descriptions are never
// printed.
func renderSessionDetail(p *presentation, result *contract.SessionResult) string {
	var b strings.Builder
	b.WriteString(p.label("会话："))
	b.WriteString(sanitizeDynamicText(result.AuthenticationSessionID))
	b.WriteString("\n")

	b.WriteString(p.label("状态："))
	b.WriteString(p.state(sessionStateText(result.State), result.State))
	b.WriteString("（")
	b.WriteString(sanitizeDynamicText(result.State))
	b.WriteString("）\n")

	b.WriteString(p.label("机构："))
	b.WriteString(sessionInstitutionText(*result))
	b.WriteString("\n")

	b.WriteString(p.label("协议："))
	b.WriteString(sanitizeDynamicText(result.AuthenticationProtocolID))
	b.WriteString("\n")

	b.WriteString(p.label("账号："))
	b.WriteString(sanitizeDynamicText(result.AccountLabel))
	b.WriteString("\n")

	if result.StateReason != nil {
		b.WriteString(p.label("原因："))
		b.WriteString(sessionStateReasonText(result.StateReason.Code))
		b.WriteString("（")
		b.WriteString(sanitizeDynamicText(result.StateReason.Code))
		b.WriteString("）\n")
	}

	if result.SelectedNetworkBinding != nil {
		b.WriteString(p.label("网络："))
		b.WriteString(sanitizeDynamicText(result.SelectedNetworkBinding.DisplayName))
		b.WriteString(" [")
		b.WriteString(sanitizeDynamicText(result.SelectedNetworkBinding.InterfaceID))
		b.WriteString("] — ")
		b.WriteString(sanitizeDynamicText(result.SelectedNetworkBinding.LocalIPv4Address))
		b.WriteString("\n")
	}

	if result.AuthenticationEstablishedAt != nil {
		b.WriteString(p.label("认证时间："))
		b.WriteString(sanitizeDynamicText(*result.AuthenticationEstablishedAt))
		b.WriteString("\n")
	}

	if result.NextRetryAt != nil {
		b.WriteString(p.label("下次重试："))
		b.WriteString(sanitizeDynamicText(*result.NextRetryAt))
		b.WriteString("\n")
	}

	if result.LastAuthenticationFailure != nil {
		failure := result.LastAuthenticationFailure
		b.WriteString(p.label("最近失败："))
		b.WriteString(failureText(failure.Code))
		b.WriteString("（")
		b.WriteString(sanitizeDynamicText(failure.Code))
		b.WriteString("）\n")

		b.WriteString(p.label("处理建议："))
		b.WriteString(recommendationText(failure.HandlingRecommendation))
		b.WriteString("（")
		b.WriteString(sanitizeDynamicText(failure.HandlingRecommendation))
		b.WriteString("）\n")
	}

	b.WriteString(p.label("更新时间："))
	b.WriteString(sanitizeDynamicText(result.UpdatedAt))
	b.WriteString("\n")

	return b.String()
}

// renderSessionList renders a complete Session list block. The heading and
// fixed labels are styled with bold cyan; mapped Session state text uses the
// same green/red/yellow/plain policy as the detail renderer; every dynamic
// value is sanitized. An empty list renders the existing plain Chinese text.
func renderSessionList(p *presentation, result *contract.SessionListResult) string {
	var b strings.Builder
	if len(result.Sessions) == 0 {
		b.WriteString("没有 Session。\n")
		return b.String()
	}
	b.WriteString(p.label(fmt.Sprintf("会话（%d）：", len(result.Sessions))))
	b.WriteString("\n")
	for _, session := range result.Sessions {
		b.WriteString("- ")
		b.WriteString(sanitizeDynamicText(session.AuthenticationSessionID))
		b.WriteString(" | ")
		b.WriteString(p.label("状态："))
		b.WriteString(p.state(sessionStateText(session.State), session.State))
		b.WriteString("（")
		b.WriteString(sanitizeDynamicText(session.State))
		b.WriteString("） | ")
		b.WriteString(p.label("机构："))
		b.WriteString(sessionInstitutionText(session))
		b.WriteString(" | ")
		b.WriteString(p.label("账号："))
		b.WriteString(sanitizeDynamicText(session.AccountLabel))
		b.WriteString(" | ")
		b.WriteString(p.label("更新时间："))
		b.WriteString(sanitizeDynamicText(session.UpdatedAt))
		b.WriteString("\n")
	}
	return b.String()
}

// renderProfileList renders a complete Profile list block. The heading and
// fixed labels are styled with bold cyan; every dynamic value is sanitized.
// An empty list renders the existing plain Chinese text.
func renderProfileList(p *presentation, result *contract.ProfileListResult) string {
	var b strings.Builder
	if len(result.Profiles) == 0 {
		b.WriteString("没有可用的机构 Profile。\n")
		return b.String()
	}
	b.WriteString(p.label(fmt.Sprintf("机构 Profile（%d）：", len(result.Profiles))))
	b.WriteString("\n")
	for _, profile := range result.Profiles {
		b.WriteString("- ")
		b.WriteString(sanitizeDynamicText(profile.InstitutionProfileID))
		b.WriteString(" | ")
		b.WriteString(p.label("名称："))
		b.WriteString(sanitizeDynamicText(profile.DisplayName))
		b.WriteString(" | ")
		b.WriteString(p.label("协议："))
		b.WriteString(sanitizeDynamicText(profile.AuthenticationProtocolID))
		b.WriteString("\n")
	}
	return b.String()
}

// writeErrorLine renders the fatal CLI error line: the static Chinese prefix
// followed by the sanitized error message and one newline. An underlying
// wrapped cause is never printed; only the error's own message text is used.
func writeErrorLine(err error) string {
	return "sidravia：错误：" + sanitizeDynamicText(err.Error()) + "\n"
}
