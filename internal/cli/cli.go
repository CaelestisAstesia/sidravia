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

// renderHelp renders a deterministic Chinese help block for c. It uses only the
// canonical command tree, the Chinese headings 用法 and 可用命令, and lists
// non-hidden canonical commands. It never contains Cobra's default English
// headings and never dispatches an operation.
func renderHelp(p *presentation, c *cobra.Command) string {
	var b strings.Builder
	b.WriteString(p.label("用法："))
	b.WriteString(helpSyntax(c.CommandPath()))
	b.WriteString("\n")
	children := visibleChildCommands(c)
	if len(children) > 0 {
		b.WriteString(p.label("可用命令："))
		b.WriteString("\n")
		for _, child := range children {
			b.WriteString("  ")
			b.WriteString(child.Name())
			b.WriteString("  ")
			b.WriteString(child.Short)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// helpSyntax returns the exact syntax line for a command path. Root and group
// commands show their full syntax; leaf commands show their own syntax.
func helpSyntax(commandPath string) string {
	switch commandPath {
	case "sidravia":
		return rootUsageLine
	case "sidravia daemon":
		return "sidravia daemon status"
	case "sidravia daemon status":
		return "sidravia daemon status"
	case "sidravia auth":
		return "sidravia auth list | sidravia auth start --profile <profile-id> --username <username> [--password-stdin] | sidravia auth status <session-id> | sidravia auth stop <session-id>"
	case "sidravia auth list":
		return "sidravia auth list"
	case "sidravia auth start":
		return "sidravia auth start --profile <profile-id> --username <username> [--password-stdin]"
	case "sidravia auth status":
		return "sidravia auth status <session-id>"
	case "sidravia auth stop":
		return "sidravia auth stop <session-id>"
	case "sidravia profile":
		return "sidravia profile list"
	case "sidravia profile list":
		return "sidravia profile list"
	default:
		return commandPath
	}
}

// visibleChildCommands returns c's non-hidden direct children in Cobra's stable
// order.
func visibleChildCommands(c *cobra.Command) []*cobra.Command {
	var visible []*cobra.Command
	for _, child := range c.Commands() {
		if child.Hidden {
			continue
		}
		visible = append(visible, child)
	}
	return visible
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
