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

// helpUsageLines renders the shared multi-line usage section. Every syntax is
// trusted static specification text and occupies its own two-space-indented
// line beneath the styled heading.
func (p *presentation) helpUsageLines(usages []string) []string {
	lines := []string{p.label("用法：")}
	for _, usage := range usages {
		lines = append(lines, "  "+usage)
	}
	return lines
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
	case "automatic_reconnect_disabled":
		return "自动重连已关闭"
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
	case "session_not_found":
		return "找不到指定的认证 Session；请运行 sidravia auth list 查看可用 Session"
	case "session_active_conflict":
		return "已有认证 Session 正在运行；请先停止或删除现有活动 Session"
	case "session_state_conflict":
		return "认证 Session 当前状态不允许该操作；请运行 sidravia auth status 查看状态"
	case "configuration_not_found":
		return "找不到指定的认证配置；请运行 sidravia config list 查看可用配置"
	case "configuration_conflict":
		return "认证配置已存在；请运行 sidravia config list 查看现有配置，再运行 sidravia help config update 查看更新方法"
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
	b.WriteString(" | ")
	b.WriteString(p.label("模式："))
	b.WriteString(sanitizeDynamicText(result.Mode))
	b.WriteString("\n")
	b.WriteString("日志：portable 模式为程序目录下 logs/sidraviad.log；安装版为当前用户缓存目录下 Sidravia/logs/sidraviad.log。\n")
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
		return sanitizeDynamicText(session.InstitutionProfileID)
	}
	return sanitizeDynamicText(session.InstitutionDisplayName) + "（" + sanitizeDynamicText(session.InstitutionProfileID) + "）"
}

// institutionDisplayName returns the human display name for an institution
// profile summary. The built-in JLU profile renders as its fixed human
// identifier; other profiles use their safe display name unchanged.
func institutionDisplayName(profileID, displayName string) string {
	if profileID == jluProfileID {
		return jluHumanLabel
	}
	return sanitizeDynamicText(displayName)
}

// networkBindingText renders the selected network binding for humans: when a
// friendly display name is present it shows "<friendly name> — <IPv4>",
// otherwise only "<IPv4>". The machine InterfaceID stays in the DTO for machine
// consumers but never appears in ordinary output.
func networkBindingText(binding *contract.SessionNetworkBinding) string {
	ipv4 := sanitizeDynamicText(binding.LocalIPv4Address)
	name := sanitizeDynamicText(binding.DisplayName)
	if name == "" {
		return ipv4
	}
	return name + " — " + ipv4
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
	if result.DisplayName != "" {
		b.WriteString(p.label("名称："))
		b.WriteString(sanitizeDynamicText(result.DisplayName))
		b.WriteString("\n")
	}

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
	b.WriteString(sanitizeDynamicText(result.AccountName))
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
		b.WriteString(networkBindingText(result.SelectedNetworkBinding))
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
		b.WriteString("没有 Session。\n下一步：运行 sidravia help auth start 开始认证。\n")
		return b.String()
	}
	b.WriteString(p.label(fmt.Sprintf("会话（%d）：", len(result.Sessions))))
	b.WriteString("\n")
	for _, session := range result.Sessions {
		b.WriteString("- ")
		b.WriteString(sanitizeDynamicText(session.AuthenticationSessionID))
		if session.DisplayName != "" {
			b.WriteString(" | ")
			b.WriteString(p.label("名称："))
			b.WriteString(sanitizeDynamicText(session.DisplayName))
		}
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
		b.WriteString(sanitizeDynamicText(session.AccountName))
		b.WriteString(" | ")
		b.WriteString(p.label("更新时间："))
		b.WriteString(sanitizeDynamicText(session.UpdatedAt))
		b.WriteString("\n")
		if session.State == "blocked_by_error" || session.State == "waiting_before_retry" {
			b.WriteString("  查看详情：sidravia auth status ")
			b.WriteString(sanitizeDynamicText(session.AuthenticationSessionID))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderProfileList renders a complete Profile list block. The heading and
// fixed labels are styled with bold cyan; every dynamic value is sanitized.
// An empty list renders the existing plain Chinese text.
func renderProfileList(p *presentation, result *contract.ProfileListResult) string {
	var b strings.Builder
	if len(result.Profiles) == 0 {
		b.WriteString("没有可用的机构 Profile。请检查完整 portable 包中的 institution-profiles，并重启 daemon。\n")
		return b.String()
	}
	b.WriteString(p.label(fmt.Sprintf("机构 Profile（%d）：", len(result.Profiles))))
	b.WriteString("\n")
	for _, profile := range result.Profiles {
		b.WriteString("- ")
		b.WriteString(sanitizeDynamicText(profile.InstitutionProfileID))
		b.WriteString(" | ")
		b.WriteString(p.label("名称："))
		b.WriteString(institutionDisplayName(profile.InstitutionProfileID, profile.DisplayName))
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

// renderDaemonStarted renders the fixed daemon started confirmation line.
func renderDaemonStarted() string {
	return "守护进程：已启动（started）\n"
}

func renderDaemonAlreadyRunning() string {
	return "守护进程：已经运行（already_running）\n"
}

// renderDaemonStopped renders the fixed daemon stopped confirmation line.
func renderDaemonStopped() string {
	return "守护进程：已停止（stopped）\n"
}

func renderDaemonAlreadyStopped() string {
	return "守护进程：已经停止（already_stopped）\n"
}

// renderDaemonRestarted renders the fixed daemon restarted confirmation line.
func renderDaemonRestarted() string {
	return "守护进程：已重启（restarted）\n"
}

func renderDaemonStartedFromStopped() string {
	return "守护进程：原先未运行，现已启动（started_from_stopped）\n"
}

func renderSessionStartResult(p *presentation, result *contract.SessionStartResult) string {
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
	b.WriteString(renderSessionDetail(p, &result.Session))
	b.WriteString("查看状态：sidravia auth status ")
	b.WriteString(sanitizeDynamicText(result.Session.AuthenticationSessionID))
	b.WriteString("\n")
	return b.String()
}

func renderSessionRemoved(result *contract.SessionRemoveResult) string {
	return "Session 已删除：" + sanitizeDynamicText(result.SessionID) + "\n"
}

func renderConfiguration(result contract.ConfigurationResult) string {
	institution := strings.ToUpper(result.InstitutionProfileID)
	if result.InstitutionDisplayName != "" {
		institution = result.InstitutionDisplayName + "（" + institution + "）"
	}
	protection := "已保护（protected）"
	if result.StorageProtection == "unprotected" {
		protection = "未保护（unprotected）"
	}
	autoLogin := "未启用"
	if result.AutoLogin {
		autoLogin = "已启用"
	}
	autoReconnect := "未启用"
	if result.AutoReconnect {
		autoReconnect = "已启用"
	}
	var b strings.Builder
	b.WriteString("配置：")
	b.WriteString(sanitizeDynamicText(result.ConfigurationID))
	b.WriteString("\n")
	if result.DisplayName != "" {
		b.WriteString("名称：")
		b.WriteString(sanitizeDynamicText(result.DisplayName))
		b.WriteString("\n")
	}
	b.WriteString("机构：")
	b.WriteString(sanitizeDynamicText(institution))
	b.WriteString("\n协议：")
	b.WriteString(sanitizeDynamicText(result.AuthenticationProtocolID))
	b.WriteString("\n账号：")
	b.WriteString(sanitizeDynamicText(result.Username))
	b.WriteString("\n凭据：已保存\n存储保护：")
	b.WriteString(protection)
	b.WriteString("\n自动登录：")
	b.WriteString(autoLogin)
	b.WriteString("\n自动重连：")
	b.WriteString(autoReconnect)
	b.WriteString("\n")
	return b.String()
}

func writeConfiguration(output io.Writer, result contract.ConfigurationResult) error {
	p := newPresentation(output)
	return wrapSafeOperation("写入配置响应", p.complete(renderConfiguration(result)))
}

func writeConfigurationList(output io.Writer, result contract.ConfigurationListResult) error {
	if len(result.Configurations) == 0 {
		p := newPresentation(output)
		return wrapSafeOperation("写入配置列表", p.complete("尚未保存认证配置。\n下一步：运行 sidravia config create 创建认证配置。\n"))
	}
	var block strings.Builder
	for index, value := range result.Configurations {
		if index > 0 {
			block.WriteString("\n")
		}
		block.WriteString(renderConfiguration(value))
	}
	p := newPresentation(output)
	return wrapSafeOperation("写入配置列表", p.complete(block.String()))
}
