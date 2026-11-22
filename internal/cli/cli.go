package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"sidravia/internal/clientbootstrap"
)

const commandUsage = "用法错误，请运行 sidraviactl help 查看帮助"

var (
	errCommandUsage = errors.New(commandUsage)
	errStatusMoved  = errors.New("命令已迁移，请使用 sidraviactl daemon status")
)

const (
	guiBootstrapSchemaVersion = 1

	guiBootstrapInvalidArgumentsCode = "invalid_arguments"
	guiBootstrapInvalidIdentityCode  = "invalid_client_identity"
	guiBootstrapUnsupportedCode      = "unsupported_platform"
	guiBootstrapModeConflictCode     = "mode_conflict"
	guiBootstrapIncompatibleCode     = "incompatible_build"
	guiBootstrapUnconfirmedCode      = "startup_unconfirmed"
	guiBootstrapFailedCode           = "bootstrap_failed"
	guiBootstrapOutputFailedCode     = "output_failed"
)

type guiBootstrapFailure struct {
	code     string
	message  string
	exitCode int
	cause    error
}

func (err *guiBootstrapFailure) Error() string { return err.message }
func (err *guiBootstrapFailure) Unwrap() error { return err.cause }

type guiBootstrapFailureDocument struct {
	SchemaVersion int `json:"schemaVersion"`
	Error         struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newGUIBootstrapFailure(code, message string, exitCode int, cause error) error {
	return &guiBootstrapFailure{code: code, message: message, exitCode: exitCode, cause: cause}
}

func invalidGUIBootstrapArguments(cause error) error {
	return newGUIBootstrapFailure(guiBootstrapInvalidArgumentsCode, "GUI bootstrap 参数无效", 2, cause)
}

func classifyGUIBootstrapFailure(cause error) error {
	var existing *guiBootstrapFailure
	if errors.As(cause, &existing) {
		return existing
	}
	switch {
	case errors.Is(cause, clientbootstrap.ErrInvalidIdentity):
		return newGUIBootstrapFailure(guiBootstrapInvalidIdentityCode, "GUI bootstrap 客户端身份无效", 10, cause)
	case errors.Is(cause, clientbootstrap.ErrDesktopUnsupported):
		return newGUIBootstrapFailure(guiBootstrapUnsupportedCode, "当前平台不支持 GUI bootstrap", 11, cause)
	case errors.Is(cause, clientbootstrap.ErrModeConflict):
		return newGUIBootstrapFailure(guiBootstrapModeConflictCode, "另一运行模式正在使用 daemon", 12, cause)
	case errors.Is(cause, clientbootstrap.ErrIncompatibleGeneration):
		return newGUIBootstrapFailure(guiBootstrapIncompatibleCode, "客户端与 daemon 构建不兼容", 13, cause)
	case clientbootstrap.IsReadinessUnconfirmed(cause):
		return newGUIBootstrapFailure(guiBootstrapUnconfirmedCode, "daemon 启动状态尚未确认", 14, cause)
	default:
		return newGUIBootstrapFailure(guiBootstrapFailedCode, "GUI bootstrap 失败", 15, cause)
	}
}

func unconfirmedGUIBootstrap(cause error) error {
	return newGUIBootstrapFailure(guiBootstrapUnconfirmedCode, "daemon 启动状态尚未确认", 14, cause)
}

func outputGUIBootstrapFailure(cause error) error {
	return newGUIBootstrapFailure(guiBootstrapOutputFailedCode, "GUI bootstrap 结果输出失败", 16, cause)
}

// Run executes the CLI with the given arguments.
func Run(args []string) error {
	return RunWithIdentity(args, "0.1.0-dev", "dev")
}

// RunWithIdentity executes the CLI using the immutable identity compiled into
// this client binary.
func RunWithIdentity(args []string, productVersion, buildID string) error {
	identity, err := clientbootstrap.NewIdentity(productVersion, buildID)
	if err != nil {
		if isGUIBootstrapPath(args) {
			return classifyGUIBootstrapFailure(err)
		}
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
	var bootstrapFailure *guiBootstrapFailure
	if errors.As(err, &bootstrapFailure) {
		var document guiBootstrapFailureDocument
		document.SchemaVersion = guiBootstrapSchemaVersion
		document.Error.Code = bootstrapFailure.code
		document.Error.Message = bootstrapFailure.message
		encoded, marshalErr := json.Marshal(document)
		if marshalErr != nil {
			return marshalErr
		}
		encoded = append(encoded, '\n')
		if n, writeErr := w.Write(encoded); writeErr != nil {
			return writeErr
		} else if n != len(encoded) {
			return io.ErrShortWrite
		}
		return nil
	}
	p := newPresentation(w)
	return p.complete(writeErrorLine(err))
}

// ExitCode returns the process exit code for a CLI result. Only the private
// GUI bootstrap contract has non-default codes; all other CLI failures retain
// the existing exit code 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var bootstrapFailure *guiBootstrapFailure
	if errors.As(err, &bootstrapFailure) {
		return bootstrapFailure.exitCode
	}
	return 1
}

type commandDependencies struct {
	identity          clientbootstrap.Identity
	daemonStatus      func() error
	daemonStart       func(logLevel string) error
	daemonStop        func() error
	daemonRestart     func(logLevel string) error
	authStart         func(authStartOptions) error
	authStatus        func(string) error
	authBusiness      func(string) error
	authStop          func(string) error
	authRestart       func(string) error
	authRemove        func(string) error
	authList          func() error
	authWatch         func(context.Context) error
	profileList       func() error
	networkInterfaces func() error

	diagnosticsExport func(string) error
	configList        func() error
	configShow        func(string) error
	configCreate      func(configCreateOptions) error
	configUpdate      func(configUpdateOptions) error
	configSetPassword func(configPasswordOptions) error
	configRemove      func(string, bool, bool) error
	guiBootstrap      guiBootstrapper
	output            io.Writer
}

func defaultCommandDependencies(identity clientbootstrap.Identity) commandDependencies {
	return commandDependencies{
		identity:      identity,
		daemonStatus:  func() error { return daemonStatus(identity) },
		daemonStart:   func(logLevel string) error { return daemonStart(identity, logLevel) },
		daemonStop:    func() error { return daemonStop(identity) },
		daemonRestart: func(logLevel string) error { return daemonRestart(identity, logLevel) },
		authStart:     func(options authStartOptions) error { return authStart(identity, options) },
		authStatus:    func(sessionID string) error { return authStatus(identity, sessionID) },
		authBusiness: func(sessionID string) error {
			return runAuthBusiness(sessionID, defaultReadOnlyAuthDependencies(identity))
		},
		authStop:    func(sessionID string) error { return authStop(identity, sessionID) },
		authRestart: func(sessionID string) error { return authRestart(identity, sessionID) },
		authRemove:  func(sessionID string) error { return authRemove(identity, sessionID) },
		authList:    func() error { return authList(identity) },
		authWatch: func(ctx context.Context) error {
			return runAuthWatch(ctx, defaultReadOnlyCommandDependencies(identity))
		},
		profileList:       func() error { return profileList(identity) },
		networkInterfaces: func() error { return networkInterfaces(identity) },
		diagnosticsExport: func(path string) error {
			return diagnosticsExport(identity, path)
		},
		configList:        func() error { return configList(identity) },
		configShow:        func(id string) error { return configShow(identity, id) },
		configCreate:      func(options configCreateOptions) error { return configCreate(identity, options) },
		configUpdate:      func(options configUpdateOptions) error { return configUpdate(identity, options) },
		configSetPassword: func(options configPasswordOptions) error { return configSetPassword(identity, options) },
		configRemove:      func(id string, yes, allow bool) error { return configRemove(identity, id, yes, allow) },
		guiBootstrap:      defaultGUIBootstrap,
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
	if isGUIBootstrapPath(args) {
		var operationErr *commandOperationError
		if errors.As(err, &operationErr) {
			return classifyGUIBootstrapFailure(operationErr.Unwrap())
		}
		return invalidGUIBootstrapArguments(err)
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

func isGUIBootstrapPath(args []string) bool {
	return len(args) >= 2 && args[0] == "gui" && args[1] == "bootstrap"
}

// usageErrorFor uses only recognized static command tokens. It never includes
// a caller-supplied value in output, while still pointing to the nearest help
// page for a malformed command invocation.
func usageErrorFor(args []string) error {
	path := ""
	if len(args) > 0 {
		switch args[0] {
		case "daemon", "auth", "profile", "config", "network", "diagnostics":
			path = args[0]
		}
	}
	if len(args) > 1 {
		valid := map[string]map[string]bool{
			"daemon":      {"status": true, "start": true, "stop": true, "restart": true},
			"auth":        {"list": true, "start": true, "status": true, "stop": true, "restart": true, "remove": true, "watch": true},
			"profile":     {"list": true},
			"network":     {"interfaces": true},
			"diagnostics": {"export": true},
			"config":      {"list": true, "show": true, "create": true, "update": true, "set-password": true, "remove": true},
		}
		if valid[path][args[1]] {
			path += " " + args[1]
		}
	}
	if path == "" {
		return errCommandUsage
	}
	return errors.New("用法错误，请运行 sidraviactl help " + path + " 查看帮助")
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
		Use:           "sidraviactl",
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
				return wrapCommandOperation(daemonCommandError("status", deps.daemonStatus()))
			},
		},
		newDaemonLogLevelCommand("start", "启动本地 daemon", func(level string) error {
			return daemonCommandError("start", deps.daemonStart(level))
		}),
		newNoArgsCommand("stop", "停止本地 daemon", func() error {
			return daemonCommandError("stop", deps.daemonStop())
		}),
		newDaemonLogLevelCommand("restart", "重启本地 daemon", func(level string) error {
			return daemonCommandError("restart", deps.daemonRestart(level))
		}),
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
		newNoArgsCommand("list", "列出 Session", deps.authList),
		newNoArgsCommand("watch", "持续查看已有 daemon 的最新 Session 与网络状态", func() error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if deps.authWatch == nil {
				return errors.New("状态监听不可用")
			}
			return deps.authWatch(ctx)
		}),
		newAuthStartCommand(deps),
		newSessionCommand("status", "显示 Session 状态", deps.authStatus),
		newSessionCommand("business", "显示服务器账户统计及最近文字通知（只读）", deps.authBusiness),
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
	profile.AddCommand(newNoArgsCommand("list", "列出机构 Profile", deps.profileList))
	network := &cobra.Command{Use: "network", Short: "查看网络观察", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return wrapCommandOperation(renderHelpCompletion(cmd)) }}
	network.AddCommand(newNoArgsCommand("interfaces", "列出已观察网卡", deps.networkInterfaces))
	diagnostics := &cobra.Command{Use: "diagnostics", Short: "导出安全诊断摘要", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return wrapCommandOperation(renderHelpCompletion(cmd)) }}
	diagnostics.AddCommand(newDiagnosticsExportCommand(deps.diagnosticsExport))
	configCommand := newConfigCommand(deps)

	bootstrap := deps.guiBootstrap
	if bootstrap == nil {
		bootstrap = defaultGUIBootstrap
	}
	root.AddCommand(daemon, auth, profile, network, diagnostics, configCommand, retiredStatus, newGUIBootstrapCommand(deps.identity, output, bootstrap), newIntegrationCommand(output, invokeInstallationManager, deps.identity), newUpdateCommand(output, func(ctx context.Context, args []string) ([]byte, error) {
		return invokeUpdater(ctx, args, deps.identity)
	}), newUpdateInternalCommand(deps.identity, output))
	root.SetHelpCommand(newHelpCommand(root))
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		p := newPresentation(c.OutOrStdout())
		*helpErr = p.complete(renderHelp(p, c))
	})
	return root
}

func newNoArgsCommand(name string, description string, operation func() error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return wrapCommandOperation(operation())
		},
	}
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
