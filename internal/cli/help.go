package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:                "help",
		Short:              "显示命令帮助",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := root
			for _, name := range args {
				next, _, err := target.Find([]string{name})
				if err != nil || next == target || next.Hidden || next.Name() != name {
					return errCommandUsage
				}
				target = next
			}
			if target != root && !target.IsAvailableCommand() {
				return errCommandUsage
			}
			target.SetOut(cmd.OutOrStdout())
			return wrapCommandOperation(renderHelpCompletion(target))
		},
	}
}

// helpChild is one entry in a help node’s 可用命令 section.
type helpChild struct {
	name  string
	short string
}

// helpNode describes one command's deterministic Chinese help. The
// presentation layer renders this specification instead of Cobra defaults.
type helpNode struct {
	description string
	usage       []string
	children    []helpChild
	args        []string
	options     []string
	examples    []string
}

// helpSpecs supplies extended help for commands with detailed usage examples.
// Commands with options registered directly on Cobra use that metadata.
var helpSpecs = map[string]helpNode{
	"sidraviactl network":            {description: "只读查看已有 daemon 接受的网络观察。", usage: []string{"sidraviactl network <command>"}, children: []helpChild{{"interfaces", "列出已观察网卡"}}},
	"sidraviactl network interfaces": {description: "列出网卡与 IPv4 候选；使用时重新核对绑定。", usage: []string{"sidraviactl network interfaces"}, examples: []string{"sidraviactl network interfaces"}},
	"sidraviactl diagnostics":        {description: "只读导出安全白名单诊断摘要；只连接已有 headless daemon。", usage: []string{"sidraviactl diagnostics <command>"}, children: []helpChild{{"export", "写入新的诊断文件"}}},
	"sidraviactl diagnostics export": {description: "将安全白名单诊断摘要写入一个新文件；不会覆盖已有路径。", usage: []string{"sidraviactl diagnostics export --output PATH"}, options: []string{"--output PATH    新诊断文件路径；目标必须尚不存在"}, examples: []string{"sidraviactl diagnostics export --output diagnostics.json"}},
	"sidraviactl": {
		description: "Sidravia 命令行客户端。",
		usage:       []string{"sidraviactl <command>"},
		children: []helpChild{
			{"daemon", "管理本地 daemon 进程"},
			{"auth", "管理认证 Session"},
			{"profile", "查看机构 Profile"},
			{"network", "查看网络观察"},
			{"diagnostics", "导出安全诊断摘要"},
			{"config", "管理认证配置"},
		},
		examples: []string{
			"sidraviactl daemon status",
			"sidraviactl auth start --profile jlu --username <username>",
			"sidraviactl auth list",
			"sidraviactl profile list",
			"sidraviactl config list",
		},
	},
	"sidraviactl daemon": {
		description: "管理本地 daemon 进程。",
		usage:       []string{"sidraviactl daemon <command>"},
		children: []helpChild{
			{"status", "显示 daemon 状态"},
			{"start", "启动本地 daemon"},
			{"stop", "停止本地 daemon"},
			{"restart", "重启本地 daemon"},
		},
	},
	"sidraviactl daemon status": {
		description: "显示本地 daemon 进程状态。",
		usage:       []string{"sidraviactl daemon status"},
		examples: []string{
			"sidraviactl daemon status",
		},
	},
	"sidraviactl daemon start": {
		description: "启动本地 daemon 进程。",
		usage:       []string{"sidraviactl daemon start [--log-level info|debug|trace]"},
		options: []string{
			"--log-level info|debug|trace：子进程日志级别，默认 debug",
		},
		examples: []string{
			"sidraviactl daemon start",
			"sidraviactl daemon start --log-level debug",
		},
	},
	"sidraviactl daemon stop": {
		description: "停止本地 daemon 进程。",
		usage:       []string{"sidraviactl daemon stop"},
		examples: []string{
			"sidraviactl daemon stop",
		},
	},
	"sidraviactl daemon restart": {
		description: "重启本地 daemon 进程。",
		usage:       []string{"sidraviactl daemon restart [--log-level info|debug|trace]"},
		options: []string{
			"--log-level info|debug|trace：子进程日志级别，默认 debug",
		},
		examples: []string{
			"sidraviactl daemon restart",
			"sidraviactl daemon restart --log-level trace",
		},
	},
	"sidraviactl auth": {
		description: "管理认证 Session。",
		usage:       []string{"sidraviactl auth <command>"},
		children: []helpChild{
			{"list", "列出 Session"},
			{"watch", "持续查看最新 Session 与网络状态"},
			{"start", "启动一次性认证 Session"},
			{"status", "显示 Session 状态"},
			{"stop", "停止 Session"},
			{"restart", "重启 Session"},
			{"remove", "删除 Session"},
		},
	},
	"sidraviactl auth list": {
		description: "列出当前 daemon 进程保留的全部 Session。",
		usage:       []string{"sidraviactl auth list"},
		examples: []string{
			"sidraviactl auth list",
		},
	},
	"sidraviactl auth watch": {
		description: "只连接已有 headless daemon，显示初始状态并持续显示最新 Session 与网络状态。daemon 可合并更新；输出不是每次状态转换的历史记录。按 Ctrl+C 停止；连接断开后请重新运行此命令。",
		usage:       []string{"sidraviactl auth watch"},
		examples:    []string{"sidraviactl auth watch"},
	},
	"sidraviactl auth start": {
		description: "启动一次性认证 Session、从配置启动，或确保已有 Session 正在运行。命令只返回初始 Snapshot，不等待认证完成。下一步：使用返回的 Session ID 运行 sidraviactl auth status <session-id>；不知道 ID 时先运行 sidraviactl auth list。",
		usage: []string{
			"sidraviactl auth start --profile <profile-id> --username <username> [--password-stdin]",
			"sidraviactl auth start --session <session-id>",
			"sidraviactl auth start --config <configuration-id>",
		},
		options: []string{
			"--profile <profile-id>：机构 Profile ID",
			"--username <username>：认证账号",
			"--password-stdin：从 stdin 读取密码",
			"--interface-id <id> 与 --local-ipv4 <IPv4>：同时指定真实绑定；不可用时等待，不回退",
			"--session <session-id>：确保已有 Session 正在运行",
			"--config <configuration-id>：从持久配置启动或确保 Session 正在运行",
		},
		examples: []string{
			"sidraviactl auth start --profile jlu --username <username>",
			"sidraviactl auth start --profile jlu --username <username> --password-stdin",
			"sidraviactl auth start --session session-1",
			"sidraviactl auth start --config campus",
		},
	},
	"sidraviactl auth status": {
		description: "显示指定 Session 的公开状态。",
		usage:       []string{"sidraviactl auth status <session-id>"},
		args: []string{
			"<session-id>：Session ID",
		},
		examples: []string{
			"sidraviactl auth status session-1",
		},
	},
	"sidraviactl auth stop": {
		description: "停止指定 Session。",
		usage:       []string{"sidraviactl auth stop <session-id>"},
		args: []string{
			"<session-id>：Session ID",
		},
		examples: []string{
			"sidraviactl auth stop session-1",
		},
	},
	"sidraviactl auth restart": {
		description: "重启指定 retained Session。",
		usage:       []string{"sidraviactl auth restart <session-id>"},
		args:        []string{"<session-id>：Session ID"},
		examples:    []string{"sidraviactl auth restart session-1"},
	},
	"sidraviactl auth remove": {
		description: "停止并删除指定 retained Session。",
		usage:       []string{"sidraviactl auth remove <session-id>"},
		args:        []string{"<session-id>：Session ID"},
		examples:    []string{"sidraviactl auth remove session-1"},
	},
	"sidraviactl profile": {
		description: "查看机构 Profile。",
		usage:       []string{"sidraviactl profile <command>"},
		children: []helpChild{
			{"list", "列出机构 Profile"},
		},
	},
	"sidraviactl profile list": {
		description: "列出已加载的机构 Profile 摘要。",
		usage:       []string{"sidraviactl profile list"},
		examples: []string{
			"sidraviactl profile list",
		},
	},
	"sidraviactl config": {
		description: "管理持久认证配置。",
		usage:       []string{"sidraviactl config <command>"},
		children:    []helpChild{{"list", "列出认证配置"}, {"show", "显示认证配置"}, {"create", "创建认证配置"}, {"update", "更新认证配置"}, {"set-password", "更新认证密码"}, {"remove", "删除认证配置"}},
	},
	"sidraviactl config list": {description: "列出认证配置。", usage: []string{"sidraviactl config list"}, examples: []string{"sidraviactl config list"}},
	"sidraviactl config show": {description: "显示认证配置。", usage: []string{"sidraviactl config show <configuration-id>"}, args: []string{"<configuration-id>：配置 ID"}, examples: []string{"sidraviactl config show campus"}},
	"sidraviactl config create": {
		description: "创建认证配置。交互模式下默认提供自动绑定，并可按已观察网卡选择接口和 IPv4；非交互模式不提示并默认自动绑定。使用时 Session 会重新核对网卡事实。",
		usage:       []string{"sidraviactl config create", "sidraviactl config create --id <id> --profile <profile-id> --username <username> --password-stdin [--name <display-name>] [--auto-login] [--auto-reconnect=false] [--allow-insecure-storage] [--protocol-override-file PATH]"},
		options:     []string{"--interface-id <id> 与 --local-ipv4 <IPv4>：同时提供以跳过交互选择并指定真实绑定；非交互未指定时自动绑定", "--id <id>：配置 ID（交互模式可输入）", "--profile <profile-id>：机构 Profile ID（交互模式可选择）", "--username <username>：认证账号（交互模式可输入）", "--name <display-name>：显示名称", "--password-stdin：从 stdin 读取密码；非交互模式必需", "--protocol-override-file PATH：读取替换用的严格 JSON 对象；保存后结束旧 Session，不自动连接", "--auto-login：启用自动登录，默认 false", "--auto-reconnect[=true|false]：自动重连，默认 true", "--allow-insecure-storage：确认未保护存储风险"},
		examples:    []string{"sidraviactl config create", "sidraviactl config create --id campus --profile jlu --username <username> --password-stdin", "sidraviactl config create --id campus --profile jlu --username <username> --password-stdin --auto-login --auto-reconnect=false"},
	},
	"sidraviactl config update": {
		description: "更新认证配置。",
		usage:       []string{"sidraviactl config update <configuration-id>", "sidraviactl config update <configuration-id> [--name <display-name>] [--profile <profile-id>] [--username <username>] [--auto-login true|false] [--auto-reconnect true|false] [--protocol-override-file PATH | --clear-protocol-override] [--allow-insecure-storage]"},
		args:        []string{"<configuration-id>：配置 ID"},
		options:     []string{"--interface-id <id> 与 --local-ipv4 <IPv4>：同时指定真实绑定", "--automatic-binding：恢复自动选择，与显式绑定互斥", "--name <display-name>：显示名称", "--profile <profile-id>：机构 Profile ID", "--username <username>：认证账号", "--auto-login true|false：设置自动登录", "--auto-reconnect true|false：设置自动重连", "--protocol-override-file PATH：替换为严格 JSON 对象；保存后结束旧 Session，不自动连接", "--clear-protocol-override：清除 Override；与文件选项互斥；保存后结束旧 Session，不自动连接", "--allow-insecure-storage：确认未保护存储风险"},
		examples:    []string{"sidraviactl config update campus --auto-login true", "sidraviactl config update campus --auto-reconnect false"},
	},
	"sidraviactl config set-password": {
		description: "更新认证密码。",
		usage:       []string{"sidraviactl config set-password <configuration-id> [--password-stdin] [--allow-insecure-storage]"},
		args:        []string{"<configuration-id>：配置 ID"},
		options:     []string{"--password-stdin：从 stdin 读取密码；非交互模式必需", "--allow-insecure-storage：确认未保护存储风险"},
		examples:    []string{"sidraviactl config set-password campus", "sidraviactl config set-password campus --password-stdin"},
	},
	"sidraviactl config remove": {
		description: "停止关联 Session 并删除认证配置。",
		usage:       []string{"sidraviactl config remove <configuration-id> [--yes] [--allow-insecure-storage]"},
		args:        []string{"<configuration-id>：配置 ID"},
		options:     []string{"--yes：非交互方式确认删除", "--allow-insecure-storage：确认未保护存储风险"},
		examples:    []string{"sidraviactl config remove campus", "sidraviactl config remove campus --yes"},
	},
}

// renderHelp renders a deterministic Chinese help block for c from the static
// help specification. It uses only the canonical command tree and the Chinese
// headings 用法 / 可用命令 / 参数 / 选项 / 示例. It never contains Cobra’s
// default English headings and never dispatches an operation.
func renderHelp(p *presentation, c *cobra.Command) string {
	node, ok := helpSpecs[c.CommandPath()]
	if !ok {
		node = helpNode{usage: []string{c.CommandPath()}}
		for parent := c; parent.HasParent(); parent = parent.Parent() {
			if parent.Hidden {
				return renderHelpNode(p, node)
			}
		}
		node = registeredHelpNode(c)
	} else if !c.HasParent() {
		// Keep the deliberate ordering and descriptions while exposing newer
		// public commands from the same tree used to dispatch them.
		children := append([]helpChild(nil), node.children...)
		for _, child := range registeredHelpChildren(c) {
			alreadyListed := false
			for _, listed := range node.children {
				if listed.name == child.name {
					alreadyListed = true
					break
				}
			}
			if !alreadyListed {
				children = append(children, child)
			}
		}
		node.children = children
	}
	return renderHelpNode(p, node)
}

func registeredHelpChildren(c *cobra.Command) []helpChild {
	var children []helpChild
	for _, child := range c.Commands() {
		if child.IsAvailableCommand() {
			children = append(children, helpChild{child.Name(), child.Short})
		}
	}
	return children
}

func registeredHelpNode(c *cobra.Command) helpNode {
	node := helpNode{description: c.Short, children: registeredHelpChildren(c)}
	usage := c.CommandPath()
	if len(node.children) > 0 {
		usage += " <command>"
	}
	c.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden || flag.Name == "help" {
			return
		}
		name := "--" + flag.Name
		if flag.NoOptDefVal == "" {
			name += " VALUE"
		}
		node.options = append(node.options, name+"    "+flag.Usage)
	})
	if len(node.options) > 0 {
		usage += " [选项]"
	}
	node.usage = []string{usage}
	return node
}

// renderHelpNode renders a help node as sections separated by one blank line.
// Only applicable sections appear, in the fixed order description, 用法,
// 可用命令, 参数, 选项, 示例. Each heading is styled; child commands and
// examples use one line per entry.
func renderHelpNode(p *presentation, node helpNode) string {
	var sections [][]string
	if node.description != "" {
		sections = append(sections, []string{node.description})
	}
	if len(node.usage) > 0 {
		sections = append(sections, p.helpUsageLines(node.usage))
	}
	if len(node.children) > 0 {
		lines := []string{p.label("可用命令：")}
		for _, child := range node.children {
			lines = append(lines, "  "+child.name+"  "+child.short)
		}
		sections = append(sections, lines)
	}
	if len(node.args) > 0 {
		sections = append(sections, append([]string{p.label("参数：")}, node.args...))
	}
	if len(node.options) > 0 {
		sections = append(sections, append([]string{p.label("选项：")}, node.options...))
	}
	if len(node.examples) > 0 {
		sections = append(sections, append([]string{p.label("示例：")}, node.examples...))
	}
	var b strings.Builder
	for index, section := range sections {
		if index > 0 {
			b.WriteString("\n")
		}
		for _, line := range section {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderHelpCompletion(cmd *cobra.Command) error {
	p := newPresentation(cmd.OutOrStdout())
	return wrapSafeOperation("显示帮助", p.complete(renderHelp(p, cmd)))
}
