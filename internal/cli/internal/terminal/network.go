package terminal

import (
	"fmt"
	"net/netip"
	"strings"

	"sidravia/internal/connection/binding"
	"sidravia/internal/ipc/contract"
)

func powerShellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func RenderNetworkInterfaces(result contract.NetworkInterfacesResult) string {
	if !result.Available {
		return "尚无已接受的网络观察。请确认已有 daemon 的网络观察状态。\n"
	}
	var output strings.Builder
	fmt.Fprintf(&output, "网卡（%d），观察 revision：%d；最后变更观察时间：%s\n", len(result.Interfaces), result.Revision, *result.ObservedAt)
	output.WriteString("列表是已观察事实；使用时会重新核对绑定，不代表连接成功。\n")
	if len(result.Interfaces) == 0 {
		output.WriteString("已观察到空网卡列表。\n")
	}
	for _, iface := range result.Interfaces {
		fmt.Fprintf(&output, "- 名称：%s | ID：%s | 状态：%s | 介质：%s | 硬件：%t | 物理连接器：%t | 过滤接口：%t | 端点接口：%t | 地址分配：%s\n", Sanitize(iface.DisplayName), Sanitize(iface.InterfaceID), iface.OperationalState, iface.PhysicalMedium, iface.HardwareBacked, iface.PhysicalConnectorPresent, iface.FilterInterface, iface.EndpointInterface, iface.AddressAssignmentMethod)
		if len(iface.IPv4Assignments) == 0 {
			output.WriteString("  无 IPv4 地址；当前不可绑定。\n")
		}
		for _, assignment := range iface.IPv4Assignments {
			category := "当前不可绑定"
			if assignment.AutomaticCandidate {
				category = "自动候选"
			} else if assignment.ExplicitBindable {
				category = "仅可显式绑定"
			}
			fmt.Fprintf(&output, "  IPv4：%s/%d | %s\n", assignment.Address, assignment.PrefixLength, category)
			if assignment.ExplicitBindable && (binding.Policy{Mode: binding.ExplicitInterfaceAndLocalIPv4, InterfaceID: iface.InterfaceID, LocalIPv4Address: netip.MustParseAddr(assignment.Address)}).Validate() == nil {
				fmt.Fprintf(&output, "  PowerShell 绑定参数：--interface-id %s --local-ipv4 %s\n", powerShellLiteral(iface.InterfaceID), powerShellLiteral(assignment.Address))
			}
		}
		if iface.OperationalState == "down" {
			output.WriteString("  接口已 Down，当前不可绑定。\n")
		}
	}
	return output.String()
}
