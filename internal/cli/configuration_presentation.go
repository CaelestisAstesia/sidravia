package cli

import (
	"fmt"
	"io"
	"strings"

	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

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
	credentialStored := "未保存"
	if result.CredentialStored {
		credentialStored = "已保存"
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
	b.WriteString("\n可用状态：")
	b.WriteString(configurationAvailabilityText(result.RuntimeAvailability))
	b.WriteString("\n账号：")
	b.WriteString(sanitizeDynamicText(result.Username))
	b.WriteString("\n凭据：")
	b.WriteString(credentialStored)
	b.WriteString("\n存储保护：")
	b.WriteString(protection)
	b.WriteString("\n自动登录：")
	b.WriteString(autoLogin)
	b.WriteString("\n自动重连：")
	b.WriteString(autoReconnect)
	b.WriteString("\n网络绑定：")
	if result.NetworkBindingPolicy.Mode == "explicit_interface_and_local_ipv4" {
		b.WriteString("指定网卡 ")
		b.WriteString(sanitizeDynamicText(result.NetworkBindingPolicy.InterfaceID))
		b.WriteString(" · ")
		b.WriteString(sanitizeDynamicText(result.NetworkBindingPolicy.LocalIPv4Address))
	} else {
		b.WriteString("自动选择可用网卡")
	}
	b.WriteString("\n")
	return b.String()
}

func writeConfiguration(output io.Writer, result contract.ConfigurationResult) error {
	p := newPresentation(output)
	return terminal.Failure("写入配置响应", p.complete(renderConfiguration(result)))
}

func writeConfigurationList(output io.Writer, result contract.ConfigurationListResult) error {
	if len(result.Configurations) == 0 {
		p := newPresentation(output)
		return terminal.Failure("写入配置列表", p.complete("尚未保存认证配置。\n下一步：运行 sidraviactl config create 创建认证配置。\n"))
	}
	var block strings.Builder
	for index, value := range result.Configurations {
		if index > 0 {
			block.WriteString("\n")
		}
		block.WriteString(renderConfiguration(value))
	}
	p := newPresentation(output)
	return terminal.Failure("写入配置列表", p.complete(block.String()))
}

func configurationAvailabilityText(value string) string {
	switch value {
	case "available":
		return "可用"
	case "profile_unavailable":
		return "机构资料不可用；可删除此配置"
	case "protocol_unavailable":
		return "认证协议不可用；可删除此配置"
	case "override_invalid":
		return "协议设置无效；请修改设置"
	default:
		return "未知"
	}
}
