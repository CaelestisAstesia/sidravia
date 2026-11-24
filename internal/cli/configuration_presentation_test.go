package cli

import (
	"bytes"
	"github.com/muesli/termenv"
	"sidravia/internal/ipc/contract"
	"strings"
	"testing"
)

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

func TestConfigurationCredentialPresentationReflectsStorageAndSanitizesDynamicValues(t *testing.T) {
	base := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "config\x1b[2J\ninjected", InstitutionProfileID: "jlu",
		InstitutionDisplayName: "吉林大学", AuthenticationProtocolID: "drcom",
		Username: "user\nmarker", StorageProtection: "protected",
	}
	base.CredentialStored = true
	saved := renderConfiguration(base)
	base.CredentialStored = false
	missing := renderConfiguration(base)

	if !strings.Contains(saved, "凭据：已保存\n") || !strings.Contains(missing, "凭据：未保存\n") {
		t.Fatalf("credential storage state was not presented truthfully: saved=%q missing=%q", saved, missing)
	}
	if strings.Replace(saved, "凭据：已保存\n", "凭据：未保存\n", 1) != missing {
		t.Fatalf("credential state changed unrelated configuration output: saved=%q missing=%q", saved, missing)
	}
	for name, output := range map[string]string{"saved": saved, "missing": missing} {
		if strings.ContainsAny(output, "\x1b") || strings.Contains(output, "\ninjected") || strings.Contains(output, "\nmarker") || strings.Contains(strings.ToLower(output), "password") {
			t.Errorf("%s configuration output unsafe: %q", name, output)
		}
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
