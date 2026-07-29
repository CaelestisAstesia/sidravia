package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"sidravia/internal/ipc/contract"
)

func readConfirmation(input io.Reader, output io.Writer, prompt string) (bool, error) {
	if err := writeAll(output, prompt); err != nil {
		return false, wrapSafeOperation("写入确认提示", err)
	}
	value, err := readPasswordLine(input)
	if err != nil {
		return false, wrapSafeOperation("读取确认", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true, nil
	case "", "n", "no":
		return false, nil
	default:
		return false, errors.New("确认输入无效")
	}
}

func readPromptLine(input io.Reader, output io.Writer, prompt string) (string, error) {
	if err := writeAll(output, prompt); err != nil {
		return "", wrapSafeOperation("写入交互提示", err)
	}
	value, err := readPasswordLine(input)
	if err != nil {
		return "", wrapSafeOperation("读取交互输入", err)
	}
	return strings.TrimSpace(value), nil
}

func selectProfile(deps authDependencies, connection daemonClient) (string, error) {
	raw, err := callConfiguration(deps, connection, contract.MethodProfileList, struct{}{})
	if err != nil {
		return "", err
	}
	result, err := decodeProfileListResult(raw)
	if err != nil {
		return "", wrapSafeOperation("解码 Profile 列表", err)
	}
	if len(result.Profiles) == 0 {
		return "", errors.New("没有可用的机构 Profile")
	}
	for index, profile := range result.Profiles {
		if err := writeAll(deps.stderr, fmt.Sprintf("%d. %s\n", index+1, sanitizeDynamicText(institutionDisplayName(profile.InstitutionProfileID, profile.DisplayName)))); err != nil {
			return "", wrapSafeOperation("写入 Profile 选择", err)
		}
	}
	value, err := readPromptLine(deps.stdin, deps.stderr, "选择 Profile：")
	if err != nil {
		return "", err
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 1 || index > len(result.Profiles) {
		return "", errors.New("Profile 选择无效")
	}
	return result.Profiles[index-1].InstitutionProfileID, nil
}
