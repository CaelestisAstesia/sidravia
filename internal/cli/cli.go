package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

const commandUsage = "用法错误，请运行 sidravia help 查看帮助"

var (
	errCommandUsage = errors.New(commandUsage)
	errStatusMoved  = errors.New("命令已迁移，请使用 sidravia daemon status")
)

// Run executes the CLI with the given arguments.
func Run(args []string) error {
	return RunWithIdentity(args, "0.1.0-dev", "dev")
}

// RunWithIdentity executes the CLI using the immutable identity compiled into
// this client binary.
func RunWithIdentity(args []string, productVersion, buildID string) error {
	identity, err := clientbootstrap.NewIdentity(productVersion, buildID)
	if err != nil {
		return err
	}
	return runCommand(args, defaultCommandDependencies(identity))
}

// WriteError writes a safe Chinese error line for err to w through the
// presentation boundary and returns the presentation write/restore error. The
// static prefix is followed by the error's own sanitized message; an
// underlying cause is never printed. Callers that already own a nonzero exit
// may ignore the returned reporting error so it cannot replace the business
// error.
func WriteError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	p := newPresentation(w)
	return p.complete(writeErrorLine(err))
}

type authStartOptions struct {
	profileID       string
	username        string
	sessionID       string
	configurationID string
	passwordStdin   bool
}

type commandDependencies struct {
	identity          clientbootstrap.Identity
	daemonStatus      func() error
	daemonStart       func(logLevel string) error
	daemonStop        func() error
	daemonRestart     func(logLevel string) error
	authStart         func(authStartOptions) error
	authStatus        func(string) error
	authStop          func(string) error
	authRestart       func(string) error
	authRemove        func(string) error
	authList          func() error
	profileList       func() error
	configList        func() error
	configShow        func(string) error
	configCreate      func(configCreateOptions) error
	configUpdate      func(configUpdateOptions) error
	configSetPassword func(configPasswordOptions) error
	configRemove      func(string, bool) error
	output            io.Writer
}

func defaultCommandDependencies(identity clientbootstrap.Identity) commandDependencies {
	return commandDependencies{
		identity:          identity,
		daemonStatus:      func() error { return daemonStatus(identity) },
		daemonStart:       func(logLevel string) error { return daemonStart(identity, logLevel) },
		daemonStop:        func() error { return daemonStop(identity) },
		daemonRestart:     func(logLevel string) error { return daemonRestart(identity, logLevel) },
		authStart:         func(options authStartOptions) error { return authStart(identity, options) },
		authStatus:        func(sessionID string) error { return authStatus(identity, sessionID) },
		authStop:          func(sessionID string) error { return authStop(identity, sessionID) },
		authRestart:       func(sessionID string) error { return authRestart(identity, sessionID) },
		authRemove:        func(sessionID string) error { return authRemove(identity, sessionID) },
		authList:          func() error { return authList(identity) },
		profileList:       func() error { return profileList(identity) },
		configList:        func() error { return configList(identity) },
		configShow:        func(id string) error { return configShow(identity, id) },
		configCreate:      func(options configCreateOptions) error { return configCreate(identity, options) },
		configUpdate:      func(options configUpdateOptions) error { return configUpdate(identity, options) },
		configSetPassword: func(options configPasswordOptions) error { return configSetPassword(identity, options) },
		configRemove:      func(id string, yes bool) error { return configRemove(identity, id, yes) },
		output:            os.Stdout,
	}
}

func runCommand(args []string, deps commandDependencies) error {
	output := deps.output
	if output == nil {
		output = io.Discard
	}

	var helpErr error
	root := newRootCommand(deps, output, &helpErr)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		if helpErr != nil {
			return wrapSafeOperation("显示帮助", helpErr)
		}
		return nil
	}

	var operationErr *commandOperationError
	if errors.As(err, &operationErr) {
		return operationErr.Unwrap()
	}
	if errors.Is(err, errStatusMoved) {
		return errStatusMoved
	}
	return usageErrorFor(args)
}

// usageErrorFor uses only recognized static command tokens. It never includes
// a caller-supplied value in output, while still pointing to the nearest help
// page for a malformed command invocation.
func usageErrorFor(args []string) error {
	path := ""
	if len(args) > 0 {
		switch args[0] {
		case "daemon", "auth", "profile", "config":
			path = args[0]
		}
	}
	if len(args) > 1 {
		valid := map[string]map[string]bool{
			"daemon":  {"status": true, "start": true, "stop": true, "restart": true},
			"auth":    {"list": true, "start": true, "status": true, "stop": true, "restart": true, "remove": true},
			"profile": {"list": true},
			"config":  {"list": true, "show": true, "create": true, "update": true, "set-password": true, "remove": true},
		}
		if valid[path][args[1]] {
			path += " " + args[1]
		}
	}
	if path == "" {
		return errCommandUsage
	}
	return errors.New("用法错误，请运行 sidravia help " + path + " 查看帮助")
}

type commandOperationError struct {
	err error
}

func (err *commandOperationError) Error() string {
	return err.err.Error()
}

func (err *commandOperationError) Unwrap() error {
	return err.err
}

func wrapCommandOperation(err error) error {
	if err == nil {
		return nil
	}
	return &commandOperationError{err: err}
}

func newRootCommand(deps commandDependencies, output io.Writer, helpErr *error) *cobra.Command {
	root := &cobra.Command{
		Use:           "sidravia",
		Short:         "Sidravia 命令行客户端",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return wrapCommandOperation(renderHelpCompletion(cmd))
		},
	}
	root.SetOut(output)
	root.SetErr(io.Discard)
	root.DisableSuggestions = true
	root.CompletionOptions.DisableDefaultCmd = true

	daemon := &cobra.Command{
		Use:   "daemon",
		Short: "管理本地 daemon 进程",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return wrapCommandOperation(renderHelpCompletion(cmd))
		},
	}
	daemon.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "显示 daemon 状态",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return wrapCommandOperation(deps.daemonStatus())
			},
		},
		newDaemonLogLevelCommand("start", "启动本地 daemon", deps.daemonStart),
		newListCommand("stop", "停止本地 daemon", deps.daemonStop),
		newDaemonLogLevelCommand("restart", "重启本地 daemon", deps.daemonRestart),
	)

	retiredStatus := &cobra.Command{
		Use:    "status",
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errCommandUsage
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			return errStatusMoved
		},
	}

	auth := &cobra.Command{
		Use:   "auth",
		Short: "管理认证 Session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return wrapCommandOperation(renderHelpCompletion(cmd))
		},
	}
	auth.AddCommand(
		newListCommand("list", "列出 Session", deps.authList),
		newAuthStartCommand(deps),
		newSessionCommand("status", "显示 Session 状态", deps.authStatus),
		newSessionCommand("stop", "停止 Session", deps.authStop),
		newSessionCommand("restart", "重启 Session", deps.authRestart),
		newSessionCommand("remove", "删除 Session", deps.authRemove),
	)

	profile := &cobra.Command{
		Use:   "profile",
		Short: "查看机构 Profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return wrapCommandOperation(renderHelpCompletion(cmd))
		},
	}
	profile.AddCommand(newListCommand("list", "列出机构 Profile", deps.profileList))
	configCommand := newConfigCommand(deps)

	root.AddCommand(daemon, auth, profile, configCommand, retiredStatus)
	root.SetHelpCommand(newHelpCommand(root))
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		p := newPresentation(c.OutOrStdout())
		*helpErr = p.complete(renderHelp(p, c))
	})
	return root
}

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
			if _, ok := helpSpecs[target.CommandPath()]; !ok {
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

// helpNode is the deterministic Chinese help specification for one command.
// The CLI presentation boundary owns it; later packages extend the same
// specification for new daemon and Session leaves instead of regenerating
// Cobra defaults.
type helpNode struct {
	description string
	usage       []string
	children    []helpChild
	args        []string
	options     []string
	examples    []string
}

// helpSpecs is the static, deterministic help specification keyed by command
// path. It is the sole source of CLI help text; Cobra’s generated defaults
// are never shown.
var helpSpecs = map[string]helpNode{
	"sidravia": {
		description: "Sidravia 命令行客户端。",
		usage:       []string{"sidravia <command>"},
		children: []helpChild{
			{"daemon", "管理本地 daemon 进程"},
			{"auth", "管理认证 Session"},
			{"profile", "查看机构 Profile"},
			{"config", "管理认证配置"},
		},
		examples: []string{
			"sidravia daemon status",
			"sidravia auth start --profile jlu --username <username>",
			"sidravia auth list",
			"sidravia profile list",
			"sidravia config list",
		},
	},
	"sidravia daemon": {
		description: "管理本地 daemon 进程。",
		usage:       []string{"sidravia daemon <command>"},
		children: []helpChild{
			{"status", "显示 daemon 状态"},
			{"start", "启动本地 daemon"},
			{"stop", "停止本地 daemon"},
			{"restart", "重启本地 daemon"},
		},
	},
	"sidravia daemon status": {
		description: "显示本地 daemon 进程状态。",
		usage:       []string{"sidravia daemon status"},
		examples: []string{
			"sidravia daemon status",
		},
	},
	"sidravia daemon start": {
		description: "启动本地 daemon 进程。",
		usage:       []string{"sidravia daemon start [--log-level info|debug|trace]"},
		options: []string{
			"--log-level info|debug|trace：子进程日志级别，默认 info",
		},
		examples: []string{
			"sidravia daemon start",
			"sidravia daemon start --log-level debug",
		},
	},
	"sidravia daemon stop": {
		description: "停止本地 daemon 进程。",
		usage:       []string{"sidravia daemon stop"},
		examples: []string{
			"sidravia daemon stop",
		},
	},
	"sidravia daemon restart": {
		description: "重启本地 daemon 进程。",
		usage:       []string{"sidravia daemon restart [--log-level info|debug|trace]"},
		options: []string{
			"--log-level info|debug|trace：子进程日志级别，默认 info",
		},
		examples: []string{
			"sidravia daemon restart",
			"sidravia daemon restart --log-level trace",
		},
	},
	"sidravia auth": {
		description: "管理认证 Session。",
		usage:       []string{"sidravia auth <command>"},
		children: []helpChild{
			{"list", "列出 Session"},
			{"start", "启动一次性认证 Session"},
			{"status", "显示 Session 状态"},
			{"stop", "停止 Session"},
			{"restart", "重启 Session"},
			{"remove", "删除 Session"},
		},
	},
	"sidravia auth list": {
		description: "列出当前 daemon 进程保留的全部 Session。",
		usage:       []string{"sidravia auth list"},
		examples: []string{
			"sidravia auth list",
		},
	},
	"sidravia auth start": {
		description: "启动一次性认证 Session、从配置启动，或确保已有 Session 正在运行。命令只返回初始 Snapshot，不等待认证完成。下一步：使用返回的 Session ID 运行 sidravia auth status <session-id>；不知道 ID 时先运行 sidravia auth list。",
		usage: []string{
			"sidravia auth start --profile <profile-id> --username <username> [--password-stdin]",
			"sidravia auth start --session <session-id>",
			"sidravia auth start --config <configuration-id>",
		},
		options: []string{
			"--profile <profile-id>：机构 Profile ID",
			"--username <username>：认证账号",
			"--password-stdin：从 stdin 读取密码",
			"--session <session-id>：确保已有 Session 正在运行",
			"--config <configuration-id>：从持久配置启动或确保 Session 正在运行",
		},
		examples: []string{
			"sidravia auth start --profile jlu --username <username>",
			"sidravia auth start --profile jlu --username <username> --password-stdin",
			"sidravia auth start --session session-1",
			"sidravia auth start --config campus",
		},
	},
	"sidravia auth status": {
		description: "显示指定 Session 的公开状态。",
		usage:       []string{"sidravia auth status <session-id>"},
		args: []string{
			"<session-id>：Session ID",
		},
		examples: []string{
			"sidravia auth status session-1",
		},
	},
	"sidravia auth stop": {
		description: "停止指定 Session。",
		usage:       []string{"sidravia auth stop <session-id>"},
		args: []string{
			"<session-id>：Session ID",
		},
		examples: []string{
			"sidravia auth stop session-1",
		},
	},
	"sidravia auth restart": {
		description: "重启指定 retained Session。",
		usage:       []string{"sidravia auth restart <session-id>"},
		args:        []string{"<session-id>：Session ID"},
		examples:    []string{"sidravia auth restart session-1"},
	},
	"sidravia auth remove": {
		description: "停止并删除指定 retained Session。",
		usage:       []string{"sidravia auth remove <session-id>"},
		args:        []string{"<session-id>：Session ID"},
		examples:    []string{"sidravia auth remove session-1"},
	},
	"sidravia profile": {
		description: "查看机构 Profile。",
		usage:       []string{"sidravia profile <command>"},
		children: []helpChild{
			{"list", "列出机构 Profile"},
		},
	},
	"sidravia profile list": {
		description: "列出已加载的机构 Profile 摘要。",
		usage:       []string{"sidravia profile list"},
		examples: []string{
			"sidravia profile list",
		},
	},
	"sidravia config": {
		description: "管理持久认证配置。",
		usage:       []string{"sidravia config <command>"},
		children:    []helpChild{{"list", "列出认证配置"}, {"show", "显示认证配置"}, {"create", "创建认证配置"}, {"update", "更新认证配置"}, {"set-password", "更新认证密码"}, {"remove", "删除认证配置"}},
	},
	"sidravia config list": {description: "列出认证配置。", usage: []string{"sidravia config list"}, examples: []string{"sidravia config list"}},
	"sidravia config show": {description: "显示认证配置。", usage: []string{"sidravia config show <configuration-id>"}, args: []string{"<configuration-id>：配置 ID"}, examples: []string{"sidravia config show campus"}},
	"sidravia config create": {
		description: "创建认证配置。",
		usage:       []string{"sidravia config create", "sidravia config create --id <id> --profile <profile-id> --username <username> --password-stdin [--name <display-name>] [--auto-login] [--auto-reconnect=false] [--allow-insecure-storage]"},
		options:     []string{"--id <id>：配置 ID（交互模式可输入）", "--profile <profile-id>：机构 Profile ID（交互模式可选择）", "--username <username>：认证账号（交互模式可输入）", "--name <display-name>：显示名称", "--password-stdin：从 stdin 读取密码；非交互模式必需", "--auto-login：启用自动登录，默认 false", "--auto-reconnect[=true|false]：自动重连，默认 true", "--allow-insecure-storage：确认未保护存储风险"},
		examples:    []string{"sidravia config create", "sidravia config create --id campus --profile jlu --username <username> --password-stdin", "sidravia config create --id campus --profile jlu --username <username> --password-stdin --auto-login --auto-reconnect=false"},
	},
	"sidravia config update": {
		description: "更新认证配置。",
		usage:       []string{"sidravia config update <configuration-id>", "sidravia config update <configuration-id> [--name <display-name>] [--profile <profile-id>] [--username <username>] [--auto-login true|false] [--auto-reconnect true|false]"},
		args:        []string{"<configuration-id>：配置 ID"},
		options:     []string{"--name <display-name>：显示名称", "--profile <profile-id>：机构 Profile ID", "--username <username>：认证账号", "--auto-login true|false：设置自动登录", "--auto-reconnect true|false：设置自动重连"},
		examples:    []string{"sidravia config update campus --auto-login true", "sidravia config update campus --auto-reconnect false"},
	},
	"sidravia config set-password": {
		description: "更新认证密码。",
		usage:       []string{"sidravia config set-password <configuration-id> [--password-stdin] [--allow-insecure-storage]"},
		args:        []string{"<configuration-id>：配置 ID"},
		options:     []string{"--password-stdin：从 stdin 读取密码；非交互模式必需", "--allow-insecure-storage：确认未保护存储风险"},
		examples:    []string{"sidravia config set-password campus", "sidravia config set-password campus --password-stdin"},
	},
	"sidravia config remove": {
		description: "停止关联 Session 并删除认证配置。",
		usage:       []string{"sidravia config remove <configuration-id> [--yes]"},
		args:        []string{"<configuration-id>：配置 ID"},
		options:     []string{"--yes：非交互方式确认删除"},
		examples:    []string{"sidravia config remove campus", "sidravia config remove campus --yes"},
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
	}
	return renderHelpNode(p, node)
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

func newListCommand(name string, description string, operation func() error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return wrapCommandOperation(operation())
		},
	}
}

func newAuthStartCommand(deps commandDependencies) *cobra.Command {
	return &cobra.Command{
		Use:                "start",
		Short:              "启动一次性认证 Session",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHelpRequest(args) {
				return wrapCommandOperation(renderHelpCompletion(cmd))
			}
			options, err := parseAuthStart(args)
			if err != nil {
				return errCommandUsage
			}
			return wrapCommandOperation(deps.authStart(options))
		},
	}
}

func newSessionCommand(name string, description string, operation func(string) error) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              description,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHelpRequest(args) {
				return wrapCommandOperation(renderHelpCompletion(cmd))
			}
			sessionID, err := parseSessionID(args)
			if err != nil {
				return errCommandUsage
			}
			return wrapCommandOperation(operation(sessionID))
		},
	}
}

func parseAuthStart(args []string) (authStartOptions, error) {
	var options authStartOptions
	var profileSet, usernameSet, sessionSet, configSet, passwordStdinSet bool

	for index := 0; index < len(args); {
		switch args[index] {
		case "--profile":
			if profileSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.profileID = args[index+1]
			profileSet = true
			index += 2
		case "--username":
			if usernameSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.username = args[index+1]
			usernameSet = true
			index += 2
		case "--password-stdin":
			if passwordStdinSet {
				return authStartOptions{}, errCommandUsage
			}
			options.passwordStdin = true
			passwordStdinSet = true
			index++
		case "--session":
			if sessionSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.sessionID = args[index+1]
			sessionSet = true
			index += 2
		case "--config":
			if configSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.configurationID = args[index+1]
			configSet = true
			index += 2
		default:
			return authStartOptions{}, errCommandUsage
		}
	}

	if sessionSet || configSet {
		if sessionSet && configSet || profileSet || usernameSet || passwordStdinSet {
			return authStartOptions{}, errCommandUsage
		}
		return options, nil
	}
	if !profileSet || !usernameSet {
		return authStartOptions{}, errCommandUsage
	}
	return options, nil
}

func newDaemonLogLevelCommand(name string, description string, operation func(logLevel string) error) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              description,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHelpRequest(args) {
				return wrapCommandOperation(renderHelpCompletion(cmd))
			}
			logLevel, err := parseDaemonLogLevel(args)
			if err != nil {
				return errCommandUsage
			}
			return wrapCommandOperation(operation(logLevel))
		},
	}
}

func parseDaemonLogLevel(args []string) (string, error) {
	logLevel := ""
	logLevelSet := false
	for index := 0; index < len(args); {
		switch args[index] {
		case "--log-level":
			if logLevelSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return "", errCommandUsage
			}
			logLevel = args[index+1]
			logLevelSet = true
			index += 2
		default:
			return "", errCommandUsage
		}
	}
	switch logLevel {
	case "", "info", "debug", "trace":
		return logLevel, nil
	default:
		return "", errCommandUsage
	}
}

func parseSessionID(args []string) (string, error) {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return "", errCommandUsage
	}
	return args[0], nil
}

// isHelpRequest reports whether args is exactly a sole --help or -h token. A
// help token combined with any other token is not a help request and remains
// an invalid argument set handled by the private argument parser.
func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h")
}

// renderHelpCompletion renders the deterministic Chinese help block for cmd
// through the presentation completion path and wraps its write/restore error
// with the safe help operation label. It is used by DisableFlagParsing leaves
// whose --help/-h tokens bypass Cobra's help flag.
func renderHelpCompletion(cmd *cobra.Command) error {
	p := newPresentation(cmd.OutOrStdout())
	return wrapSafeOperation("显示帮助", p.complete(renderHelp(p, cmd)))
}

// writeStatus renders a daemon.status response to w through the presentation
// boundary. A non-OK response is mapped to fixed Chinese guidance without the
// daemon message; otherwise the StatusResult is decoded and rendered as the
// canonical one-line Chinese format. It never writes a partial success line.
func writeStatus(w io.Writer, resp contract.Response) error {
	if !resp.OK {
		code := ""
		if resp.Error != nil {
			code = resp.Error.Code
		}
		return errors.New(ipcErrorText(code))
	}

	var result contract.StatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return wrapSafeOperation("解码 daemon 状态响应", err)
	}

	p := newPresentation(w)
	return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonStatus(p, &result)))
}
