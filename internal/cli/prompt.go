package cli

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
)

func selectProfile(deps authDependencies, connection daemonconn.Client) (string, error) {
	raw, err := callConfiguration(deps, connection, contract.MethodProfileList, struct{}{})
	if err != nil {
		return "", err
	}
	result, err := decodeProfileListResult(raw)
	if err != nil {
		return "", terminal.Failure("解码 Profile 列表", err)
	}
	if len(result.Profiles) == 0 {
		return "", errors.New("没有可用的机构 Profile")
	}
	for index, profile := range result.Profiles {
		if err := terminal.Write(deps.stderr, fmt.Sprintf("%d. %s\n", index+1, sanitizeDynamicText(institutionDisplayName(profile.InstitutionProfileID, profile.DisplayName)))); err != nil {
			return "", terminal.Failure("写入 Profile 选择", err)
		}
	}
	value, err := terminal.ReadPrompt(deps.stdin, deps.stderr, "选择 Profile：")
	if err != nil {
		return "", err
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 1 || index > len(result.Profiles) {
		return "", errors.New("Profile 选择无效")
	}
	return result.Profiles[index-1].InstitutionProfileID, nil
}

type networkBindingChoice struct {
	interfaceID string
	address     string
	prefix      uint8
	displayName string
	automatic   bool
}

func selectNetworkBinding(deps authDependencies, connection daemonconn.Client) (contract.NetworkBindingPolicy, error) {
	raw, err := callConfiguration(deps, connection, contract.MethodNetworkInterfaces, struct{}{})
	if err != nil {
		return contract.NetworkBindingPolicy{}, err
	}
	snapshot, err := contract.DecodeNetworkInterfacesResult(raw)
	if err != nil {
		return contract.NetworkBindingPolicy{}, terminal.Failure("解码网卡列表", err)
	}
	choices := make([]networkBindingChoice, 0)
	if snapshot.Available {
		byPair := make(map[string]networkBindingChoice)
		for _, iface := range snapshot.Interfaces {
			if iface.OperationalState != "up" {
				continue
			}
			for _, assignment := range iface.IPv4Assignments {
				if !assignment.ExplicitBindable {
					continue
				}
				policy := contract.NetworkBindingPolicy{Mode: "explicit_interface_and_local_ipv4", InterfaceID: iface.InterfaceID, LocalIPv4Address: assignment.Address}
				if _, err := policy.Domain(); err != nil {
					return contract.NetworkBindingPolicy{}, terminal.Failure("校验网卡绑定候选", err)
				}
				key := iface.InterfaceID + "\x00" + assignment.Address
				choice := networkBindingChoice{
					interfaceID: iface.InterfaceID,
					address:     assignment.Address,
					prefix:      assignment.PrefixLength,
					displayName: iface.DisplayName,
					automatic:   assignment.AutomaticCandidate,
				}
				if existing, ok := byPair[key]; ok {
					if choice.prefix < existing.prefix {
						existing.prefix = choice.prefix
					}
					existing.automatic = existing.automatic || choice.automatic
					byPair[key] = existing
				} else {
					byPair[key] = choice
				}
			}
		}
		for _, choice := range byPair {
			choices = append(choices, choice)
		}
		sort.Slice(choices, func(i, j int) bool {
			if choices[i].interfaceID != choices[j].interfaceID {
				return choices[i].interfaceID < choices[j].interfaceID
			}
			left, _ := netip.ParseAddr(choices[i].address)
			right, _ := netip.ParseAddr(choices[j].address)
			return left.Less(right)
		})
	}
	if err := terminal.Write(deps.stderr, "0. 自动绑定（使用时由 Session 按当前观察重新选择）\n"); err != nil {
		return contract.NetworkBindingPolicy{}, terminal.Failure("写入网卡绑定选择", err)
	}
	if !snapshot.Available {
		if err := terminal.Write(deps.stderr, "当前没有已接受的网络观察；本次将使用自动绑定。\n"); err != nil {
			return contract.NetworkBindingPolicy{}, terminal.Failure("写入网卡绑定说明", err)
		}
	} else if len(choices) == 0 {
		if err := terminal.Write(deps.stderr, "当前观察中没有可显式绑定的 Up 接口 IPv4 地址；本次将使用自动绑定。\n"); err != nil {
			return contract.NetworkBindingPolicy{}, terminal.Failure("写入网卡绑定说明", err)
		}
	}
	for index, choice := range choices {
		category := "仅可显式绑定"
		if choice.automatic {
			category = "自动候选，也可显式绑定"
		}
		line := fmt.Sprintf("%d. %s | ID：%s | IPv4：%s/%d | %s\n", index+1, sanitizeDynamicText(choice.displayName), sanitizeDynamicText(choice.interfaceID), choice.address, choice.prefix, category)
		if err := terminal.Write(deps.stderr, line); err != nil {
			return contract.NetworkBindingPolicy{}, terminal.Failure("写入网卡绑定选择", err)
		}
	}
	if err := terminal.Write(deps.stderr, "网卡事实来自最近一次已接受观察；Session 使用时会重新核对，选择不保证届时仍可用。\n"); err != nil {
		return contract.NetworkBindingPolicy{}, terminal.Failure("写入网卡绑定说明", err)
	}
	value, err := readChoiceLine(deps.stdin, deps.stderr, "选择绑定方式 [0]：")
	if err != nil {
		return contract.NetworkBindingPolicy{}, err
	}
	if value == "" {
		value = "0"
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 0 || index > len(choices) {
		return contract.NetworkBindingPolicy{}, errors.New("网卡绑定选择无效")
	}
	if index == 0 {
		return contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy}, nil
	}
	choice := choices[index-1]
	return contract.NetworkBindingPolicy{Mode: "explicit_interface_and_local_ipv4", InterfaceID: choice.interfaceID, LocalIPv4Address: choice.address}, nil
}

const maxChoiceLineBytes = 64

// readChoiceLine reads exactly one bounded line without buffering bytes that
// may belong to the following password input. Unlike existing password and
// confirmation readers, EOF is never a choice or an implicit default.
func readChoiceLine(input io.Reader, output io.Writer, prompt string) (string, error) {
	if err := terminal.Write(output, prompt); err != nil {
		return "", terminal.Failure("写入选择提示", err)
	}
	line := make([]byte, 0, maxChoiceLineBytes)
	var one [1]byte
	for {
		n, err := input.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				return strings.TrimSpace(string(line)), nil
			}
			if len(line) == maxChoiceLineBytes {
				return "", errors.New("网卡绑定选择过长")
			}
			line = append(line, one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", terminal.Failure("读取网卡绑定选择未收到换行", err)
			}
			return "", terminal.Failure("读取网卡绑定选择", err)
		}
		if n == 0 {
			return "", terminal.Failure("读取网卡绑定选择", io.ErrNoProgress)
		}
	}
}
