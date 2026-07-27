package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

const rootUsageLine = "sidravia daemon status | sidravia auth list | sidravia auth start --profile <profile-id> --username <username> [--password-stdin] | sidravia auth status <session-id> | sidravia auth stop <session-id> | sidravia profile list"

const commandUsage = "用法：" + rootUsageLine

var (
	errCommandUsage = errors.New(commandUsage)
	errStatusMoved  = errors.New("命令已迁移，请使用 sidravia daemon status")
)

// Run executes the CLI with the given arguments.
func Run(args []string) error {
	return runCommand(args, defaultCommandDependencies())
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
	profileID     string
	username      string
	passwordStdin bool
}

type commandDependencies struct {
	status      func() error
	authStart   func(authStartOptions) error
	authStatus  func(string) error
	authStop    func(string) error
	authList    func() error
	profileList func() error
	output      io.Writer
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		status:      status,
		authStart:   authStart,
		authStatus:  authStatus,
		authStop:    authStop,
		authList:    authList,
		profileList: profileList,
		output:      os.Stdout,
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
	return errCommandUsage
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
		RunE: func(*cobra.Command, []string) error {
			return errCommandUsage
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
		RunE: func(*cobra.Command, []string) error {
			return errCommandUsage
		},
	}
	daemon.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "显示 daemon 状态",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return wrapCommandOperation(deps.status())
		},
	})

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
		RunE: func(*cobra.Command, []string) error {
			return errCommandUsage
		},
	}
	auth.AddCommand(
		newListCommand("list", "列出 Session", deps.authList),
		newAuthStartCommand(deps),
		newSessionCommand("status", "显示 Session 状态", deps.authStatus),
		newSessionCommand("stop", "停止 Session", deps.authStop),
	)

	profile := &cobra.Command{
		Use:   "profile",
		Short: "查看机构 Profile",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errCommandUsage
		},
	}
	profile.AddCommand(newListCommand("list", "列出机构 Profile", deps.profileList))

	root.AddCommand(daemon, auth, profile, retiredStatus)
	// Cobra lazily injects an English "help" command. Replace it with a hidden
	// one so it never appears in the canonical 可用命令 listing, while keeping
	// help requests routed through the deterministic Chinese renderer below.
	root.SetHelpCommand(&cobra.Command{
		Use:    "help",
		Short:  "关于命令的帮助",
		Hidden: true,
	})
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		p := newPresentation(c.OutOrStdout())
		*helpErr = p.complete(renderHelp(p, c))
	})
	return root
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
	usage       string
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
		usage:       rootUsageLine,
		children: []helpChild{
			{"daemon", "管理本地 daemon 进程"},
			{"auth", "管理认证 Session"},
			{"profile", "查看机构 Profile"},
		},
		examples: []string{
			"sidravia daemon status",
			"sidravia auth start --profile jlu --username <username>",
			"sidravia auth list",
			"sidravia profile list",
		},
	},
	"sidravia daemon": {
		description: "管理本地 daemon 进程。",
		usage:       "sidravia daemon status",
		children: []helpChild{
			{"status", "显示 daemon 状态"},
		},
	},
	"sidravia daemon status": {
		description: "显示本地 daemon 进程状态。",
		usage:       "sidravia daemon status",
		examples: []string{
			"sidravia daemon status",
		},
	},
	"sidravia auth": {
		description: "管理认证 Session。",
		usage:       "sidravia auth list | sidravia auth start --profile <profile-id> --username <username> [--password-stdin] | sidravia auth status <session-id> | sidravia auth stop <session-id>",
		children: []helpChild{
			{"list", "列出 Session"},
			{"start", "启动一次性认证 Session"},
			{"status", "显示 Session 状态"},
			{"stop", "停止 Session"},
		},
	},
	"sidravia auth list": {
		description: "列出当前 daemon 进程保留的全部 Session。",
		usage:       "sidravia auth list",
		examples: []string{
			"sidravia auth list",
		},
	},
	"sidravia auth start": {
		description: "启动一次性认证 Session。",
		usage:       "sidravia auth start --profile <profile-id> --username <username> [--password-stdin]",
		options: []string{
			"--profile <profile-id>：机构 Profile ID",
			"--username <username>：认证账号",
			"--password-stdin：从 stdin 读取密码",
		},
		examples: []string{
			"sidravia auth start --profile jlu --username <username>",
			"sidravia auth start --profile jlu --username <username> --password-stdin",
		},
	},
	"sidravia auth status": {
		description: "显示指定 Session 的公开状态。",
		usage:       "sidravia auth status <session-id>",
		args: []string{
			"<session-id>：Session ID",
		},
		examples: []string{
			"sidravia auth status session-1",
		},
	},
	"sidravia auth stop": {
		description: "停止指定 Session。",
		usage:       "sidravia auth stop <session-id>",
		args: []string{
			"<session-id>：Session ID",
		},
		examples: []string{
			"sidravia auth stop session-1",
		},
	},
	"sidravia profile": {
		description: "查看机构 Profile。",
		usage:       "sidravia profile list",
		children: []helpChild{
			{"list", "列出机构 Profile"},
		},
	},
	"sidravia profile list": {
		description: "列出已加载的机构 Profile 摘要。",
		usage:       "sidravia profile list",
		examples: []string{
			"sidravia profile list",
		},
	},
}

// renderHelp renders a deterministic Chinese help block for c from the static
// help specification. It uses only the canonical command tree and the Chinese
// headings 用法 / 可用命令 / 参数 / 选项 / 示例. It never contains Cobra’s
// default English headings and never dispatches an operation.
func renderHelp(p *presentation, c *cobra.Command) string {
	node, ok := helpSpecs[c.CommandPath()]
	if !ok {
		node = helpNode{usage: c.CommandPath()}
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
	if node.usage != "" {
		sections = append(sections, []string{p.label("用法：") + node.usage})
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
	var profileSet, usernameSet, passwordStdinSet bool

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
		default:
			return authStartOptions{}, errCommandUsage
		}
	}

	if !profileSet || !usernameSet {
		return authStartOptions{}, errCommandUsage
	}
	return options, nil
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

func status() error {
	return runStatus(defaultStatusDependencies())
}

// statusDependencies bundles the private seams that the status command relies
// on. Behavior tests substitute individual fields without touching any
// package-global state.
type statusDependencies struct {
	runtimeInfoPath func() (string, error)
	readRuntimeInfo func(path string) (contract.RuntimeInfo, error)
	connectAndPrint func(info contract.RuntimeInfo) error
	startDaemon     func() error
	totalWait       time.Duration
	pollInterval    time.Duration
}

// defaultStatusDependencies wires the status command to its real production
// collaborators.
func defaultStatusDependencies() statusDependencies {
	return statusDependencies{
		runtimeInfoPath: runtimeInfoPath,
		readRuntimeInfo: readRuntimeInfo,
		connectAndPrint: connectAndPrint,
		startDaemon:     startDaemon,
		totalWait:       5 * time.Second,
		pollInterval:    200 * time.Millisecond,
	}
}

type discoveryDependencies struct {
	runtimeInfoPath func() (string, error)
	readRuntimeInfo func(path string) (contract.RuntimeInfo, error)
	startDaemon     func() error
	totalWait       time.Duration
	pollInterval    time.Duration
}

func defaultDiscoveryDependencies() discoveryDependencies {
	return discoveryDependencies{
		runtimeInfoPath: runtimeInfoPath,
		readRuntimeInfo: readRuntimeInfo,
		startDaemon:     startDaemon,
		totalWait:       5 * time.Second,
		pollInterval:    200 * time.Millisecond,
	}
}

// runStatus drives the status command against the supplied dependencies,
// preserving the production control flow: try a hot connection first, start the
// daemon at most once, then poll until the daemon is reachable or totalWait
// elapses.
func runStatus(deps statusDependencies) error {
	return discoverDaemon(discoveryDependencies{
		runtimeInfoPath: deps.runtimeInfoPath,
		readRuntimeInfo: deps.readRuntimeInfo,
		startDaemon:     deps.startDaemon,
		totalWait:       deps.totalWait,
		pollInterval:    deps.pollInterval,
	}, deps.connectAndPrint)
}

func discoverDaemon(deps discoveryDependencies, operation func(contract.RuntimeInfo) error) error {
	infoPath, err := deps.runtimeInfoPath()
	if err != nil {
		return wrapSafeOperation("运行信息路径", err)
	}

	// Try to connect to an already-running daemon.
	info, err := deps.readRuntimeInfo(infoPath)
	if err == nil {
		if err := operation(info); err == nil {
			return nil
		}
	}

	// Daemon not reachable - start it.
	if err := deps.startDaemon(); err != nil {
		return wrapSafeOperation("启动 sidraviad", err)
	}

	// Wait up to totalWait for the daemon to write runtime info and accept connections.
	ctx, cancel := context.WithTimeout(context.Background(), deps.totalWait)
	defer cancel()

	ticker := time.NewTicker(deps.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errors.New("等待 sidraviad 超时")
		case <-ticker.C:
			info, err := deps.readRuntimeInfo(infoPath)
			if err != nil {
				continue
			}
			if err := operation(info); err != nil {
				continue
			}
			return nil
		}
	}
}

func runtimeInfoPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Sidravia", "runtime.json"), nil
}

func readRuntimeInfo(path string) (contract.RuntimeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	return contract.DecodeRuntimeInfo(data)
}

func connectAndPrint(info contract.RuntimeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
	if err != nil {
		return err
	}
	defer c.Close()

	resp, err := c.Call(ctx, contract.MethodDaemonStatus, nil)
	if err != nil {
		return err
	}
	return writeStatus(os.Stdout, resp)
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

func startDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	daemonPath := filepath.Join(dir, "sidraviad.exe")

	cmd := exec.Command(daemonPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
