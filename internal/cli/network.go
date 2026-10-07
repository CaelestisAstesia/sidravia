package cli

import (
	"fmt"
	"strings"

	"net/netip"
	"sidravia/internal/clientbootstrap"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/ipc/contract"
)

func networkInterfaces(identity clientbootstrap.Identity) error {
	return runNetworkInterfaces(defaultListDependencies(identity))
}
func runNetworkInterfaces(deps listDependencies) error {
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		response, err := callList(connection, deps.connection.callTimeout, contract.MethodNetworkInterfaces)
		if err != nil {
			return wrapSafeOperation("查询网卡", err)
		}
		result, err := contract.DecodeNetworkInterfacesResult(response)
		if err != nil {
			return wrapSafeOperation("解码网卡列表", err)
		}
		p := newPresentation(deps.stdout)
		return wrapSafeOperation("写入网卡列表", p.complete(renderNetworkInterfaces(result)))
	})
}
func powerShellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func renderNetworkInterfaces(result contract.NetworkInterfacesResult) string {
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
		fmt.Fprintf(&output, "- 名称：%s | ID：%s | 状态：%s | 介质：%s | 硬件：%t | 物理连接器：%t | 过滤接口：%t | 端点接口：%t | 地址分配：%s\n", sanitizeDynamicText(iface.DisplayName), sanitizeDynamicText(iface.InterfaceID), iface.OperationalState, iface.PhysicalMedium, iface.HardwareBacked, iface.PhysicalConnectorPresent, iface.FilterInterface, iface.EndpointInterface, iface.AddressAssignmentMethod)
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
			if assignment.ExplicitBindable && (session.NetworkBindingPolicy{Mode: session.ExplicitInterfaceAndLocalIPv4, InterfaceID: iface.InterfaceID, LocalIPv4Address: netip.MustParseAddr(assignment.Address)}).Validate() == nil {
				fmt.Fprintf(&output, "  PowerShell 绑定参数：--interface-id %s --local-ipv4 %s\n", powerShellLiteral(iface.InterfaceID), powerShellLiteral(assignment.Address))
			}
		}
		if iface.OperationalState == "down" {
			output.WriteString("  接口已 Down，当前不可绑定。\n")
		}
	}
	return output.String()
}
