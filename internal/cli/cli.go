package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const commandUsage = "usage: sidravia daemon status | sidravia auth start --profile <profile-id> --username <username> [--password-stdin] | sidravia auth status <session-id> | sidravia auth stop <session-id>"

var (
	errCommandUsage = errors.New(commandUsage)
	errStatusMoved  = errors.New("命令已迁移，请使用 sidravia daemon status")
)

// Run executes the CLI with the given arguments.
func Run(args []string) error {
	return runCommand(args, defaultCommandDependencies())
}

type authStartOptions struct {
	profileID     string
	username      string
	passwordStdin bool
}

type commandDependencies struct {
	status     func() error
	authStart  func(authStartOptions) error
	authStatus func(string) error
	authStop   func(string) error
	output     io.Writer
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		status:     status,
		authStart:  authStart,
		authStatus: authStatus,
		authStop:   authStop,
		output:     os.Stdout,
	}
}

func runCommand(args []string, deps commandDependencies) error {
	output := deps.output
	if output == nil {
		output = io.Discard
	}

	root := newRootCommand(deps, output)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
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

func newRootCommand(deps commandDependencies, output io.Writer) *cobra.Command {
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
		newAuthStartCommand(deps),
		newSessionCommand("status", "显示 Session 状态", deps.authStatus),
		newSessionCommand("stop", "停止 Session", deps.authStop),
	)

	root.AddCommand(daemon, auth, retiredStatus)
	return root
}

func newAuthStartCommand(deps commandDependencies) *cobra.Command {
	return &cobra.Command{
		Use:                "start",
		Short:              "启动一次性认证 Session",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
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
		RunE: func(_ *cobra.Command, args []string) error {
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
		return fmt.Errorf("runtime info path: %w", err)
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
		return fmt.Errorf("failed to start sidraviad: %w", err)
	}

	// Wait up to totalWait for the daemon to write runtime info and accept connections.
	ctx, cancel := context.WithTimeout(context.Background(), deps.totalWait)
	defer cancel()

	ticker := time.NewTicker(deps.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for sidraviad")
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

// writeStatus renders a daemon.status response to w. A non-OK response is a
// daemon error; otherwise the StatusResult is decoded and printed in the
// canonical one-line format. It never writes a partial success line.
func writeStatus(w io.Writer, resp contract.Response) error {
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error.Message)
	}

	var result contract.StatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return err
	}

	fmt.Fprintf(w, "sidraviad %s (%s) pid=%d status=%s\n",
		result.ProductVersion, result.BuildID, result.PID, result.Status)
	return nil
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
