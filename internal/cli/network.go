package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
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

func newNetworkDiagnoseCommand(deps commandDependencies) *cobra.Command {
	var request contract.NetworkDiagnosePayload
	command := &cobra.Command{Use: "diagnose", Short: "诊断受信认证目标", Args: cobra.NoArgs}
	command.Flags().StringVar(&request.ConfigurationID, "config", "", "已有配置")
	command.Flags().StringVar(&request.SessionID, "session", "", "保留 Session")
	command.Flags().BoolVar(&request.Probe, "probe", false, "请求一次有界 IP 回显")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("config") && !cmd.Flags().Changed("session") && !cmd.Flags().Changed("probe") {
			return wrapCommandOperation(renderHelpCompletion(cmd))
		}
		if cmd.Flags().Changed("config") == cmd.Flags().Changed("session") {
			return errCommandUsage
		}
		raw, _ := json.Marshal(request)
		if _, err := contract.DecodeNetworkDiagnosePayload(raw); err != nil {
			return errCommandUsage
		}
		return wrapCommandOperation(deps.networkDiagnose(request))
	}
	return command
}
func runNetworkDiagnose(deps listDependencies, request contract.NetworkDiagnosePayload) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err := contract.DecodeNetworkDiagnosePayload(payload); err != nil {
		return wrapSafeOperation("诊断参数", err)
	}
	return withDaemonClient(deps.connection, func(connection daemonClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), deps.connection.callTimeout)
		defer cancel()
		response, err := connection.Call(ctx, contract.MethodNetworkDiagnose, payload)
		if err != nil {
			return wrapSafeOperation("网络诊断", err)
		}
		if !response.OK {
			code := ""
			if response.Error != nil {
				code = response.Error.Code
			}
			return wrapSafeOperation("网络诊断", errors.New(ipcErrorText(code)))
		}
		result, err := contract.DecodeNetworkDiagnoseResult(response.Result)
		if err != nil {
			return wrapSafeOperation("解码网络诊断", err)
		}
		return wrapSafeOperation("写入网络诊断", newPresentation(deps.stdout).complete(renderNetworkDiagnosis(result)))
	})
}
func renderNetworkDiagnosis(result contract.NetworkDiagnoseResult) string {
	var output strings.Builder
	basis := map[string]string{"session_binding": "当前 Session 选择", "configuration_explicit": "配置显式绑定", "os_route_proposal": "OS 路由提案"}[result.SelectionBasis]
	fmt.Fprintf(&output, "网络诊断；完成时间：%s；选择依据：%s\n", result.ObservedAt, basis)
	if result.Target != nil {
		fmt.Fprintf(&output, "受信目标：%s:%d\n", result.Target.Address, result.Target.Port)
	}
	status := map[string]string{"available": "路由可观察", "unsupported": "不支持", "binding_unavailable": "绑定不可用", "route_unavailable": "路由不可用"}[result.Status]
	fmt.Fprintf(&output, "状态：%s\n", status)
	if result.UnsupportedReason != nil {
		fmt.Fprintf(&output, "原因：%s\n", map[string]string{"platform": "当前平台", "protocol": "协议未提供诊断目标", "destination": "目标不支持此诊断"}[*result.UnsupportedReason])
	}
	if result.Route != nil {
		r := result.Route
		fmt.Fprintf(&output, "OS 路由：接口 ID %s；索引 %d；源 IPv4 %s；目标前缀 %s；下一跳 %s\n", sanitizeDynamicText(r.InterfaceID), r.InterfaceIndex, r.SourceIPv4, r.DestinationPrefix, r.NextHopIPv4)
		fmt.Fprintf(&output, "度量：路由 %d + 接口 %d = 有效 %d\n", r.RouteMetric, r.InterfaceMetric, r.EffectiveMetric)
		fmt.Fprintf(&output, "IP 回显：%s", map[string]string{"not_requested": "未请求", "reachable": "收到回复", "no_reply": "无回复", "unreachable": "不可达", "failed": "失败"}[result.Probe.Status])
		if result.Probe.RoundTripTimeMs != nil {
			fmt.Fprintf(&output, "；往返 %d ms", *result.Probe.RoundTripTimeMs)
		}
		output.WriteByte('\n')
	}
	if result.ProtocolSocket != nil {
		s := result.ProtocolSocket
		fmt.Fprintf(&output, "最近实际协议 socket：%s；Run generation %d\n", s.State, s.RunGeneration)
		if s.UpdatedAt != nil {
			fmt.Fprintf(&output, "socket 观察时间：%s\n", *s.UpdatedAt)
		}
		if s.LocalEndpoint != nil {
			fmt.Fprintf(&output, "实际本地端点：%s:%d；实际远端：%s:%d\n", s.LocalEndpoint.Address, s.LocalEndpoint.Port, s.RemoteEndpoint.Address, s.RemoteEndpoint.Port)
		}
		output.WriteString("实际 socket 属于所示 Run，可能与当前选择不同。\n")
	}
	output.WriteString("IP 回显不证明认证成功或 Internet 可用。\n")
	return output.String()
}
