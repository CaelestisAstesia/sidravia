// Package clientbootstrap owns discovery and lifecycle coordination for the
// local sidraviad process. CLI presentation deliberately remains outside it.
package clientbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
)

const (
	ensureTotalWait    = 5 * time.Second
	ensurePollInterval = 200 * time.Millisecond
)

// Client is the direct daemon connection consumed by CLI operations.
type Client interface {
	Call(context.Context, string, json.RawMessage) (contract.Response, error)
	Close() error
}

// Identity is the immutable product identity compiled into a local client.
// Runtime information locates a daemon generation; it never supplies the
// client's identity.
type Identity struct {
	ProductVersion string
	BuildID        string
}

var (
	ErrInvalidIdentity        = errors.New("invalid client identity")
	ErrIncompatibleGeneration = errors.New("incompatible daemon generation")
	ErrModeConflict           = errors.New("守护进程正在由另一种模式使用；请先退出当前模式")
	ErrDesktopUnsupported     = errors.New("图形界面 bootstrap 仅支持 Windows")
)

// NewIdentity validates the identity a client presents to the daemon.
func NewIdentity(productVersion, buildID string) (Identity, error) {
	if strings.TrimSpace(productVersion) == "" || strings.TrimSpace(buildID) == "" {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{ProductVersion: productVersion, BuildID: buildID}, nil
}

func (identity Identity) validate() error {
	_, err := NewIdentity(identity.ProductVersion, identity.BuildID)
	return err
}

type daemonLaunch struct{ exited <-chan error }

type safeOperationError struct {
	label string
	cause error
}

func (e *safeOperationError) Error() string { return e.label + "失败" }
func (e *safeOperationError) Unwrap() error { return e.cause }
func wrapSafeOperation(label string, cause error) error {
	if cause == nil {
		return nil
	}
	return &safeOperationError{label: label, cause: cause}
}

// ProbeState is the read-only state of the generation described by runtime
// information. Probe never changes runtime state or starts a process.
type ProbeState int

const (
	ProbeStopped ProbeState = iota
	ProbeReachable
	ProbeUnreachable
	ProbeMalformed
	ProbeIncompatible
)

type ProbeResult struct {
	Info   contract.RuntimeInfo
	State  ProbeState
	Status *contract.StatusResult
}

type StartOutcome string

const (
	Started        StartOutcome = "started"
	AlreadyRunning StartOutcome = "already_running"
)

type dependencies struct {
	runtimeInfoPath func() (string, error)
	readRuntimeInfo func(string) (contract.RuntimeInfo, error)
	connect         func(context.Context, contract.RuntimeInfo, Identity) (Client, error)
	launch          func(launchcontract.Options, string) (daemonLaunch, error)
	callTimeout     time.Duration
	totalWait       time.Duration
	pollInterval    time.Duration
}

func defaultDependencies() dependencies {
	return dependencies{
		runtimeInfoPath: func() (string, error) {
			layout, err := productlayout.Resolve()
			if err != nil {
				return "", err
			}
			return layout.RuntimeInfoPath, nil
		},
		readRuntimeInfo: func(path string) (contract.RuntimeInfo, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return contract.RuntimeInfo{}, err
			}
			return contract.DecodeRuntimeInfo(data)
		},
		connect: func(ctx context.Context, info contract.RuntimeInfo, identity Identity) (Client, error) {
			return client.Connect(ctx, info.Endpoint, info.Token, identity.BuildID)
		},
		launch:       launchDaemonProcess,
		callTimeout:  2 * time.Second,
		totalWait:    ensureTotalWait,
		pollInterval: ensurePollInterval,
	}
}

// Probe is read-only and never starts, stops, removes or rewrites runtime state.
func Probe(identity Identity) (ProbeResult, error) { return probe(identity, defaultDependencies()) }

func probe(identity Identity, deps dependencies) (ProbeResult, error) {
	if err := identity.validate(); err != nil {
		return ProbeResult{}, err
	}
	path, err := deps.runtimeInfoPath()
	if err != nil {
		return ProbeResult{}, wrapSafeOperation("运行信息路径", err)
	}
	info, err := deps.readRuntimeInfo(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProbeResult{State: ProbeStopped}, nil
		}
		return ProbeResult{State: ProbeMalformed}, nil
	}
	if !matchesIdentity(identity, info) {
		return ProbeResult{Info: info, State: ProbeIncompatible}, nil
	}
	status, ok := statusFor(identity, deps, info)
	if !ok {
		return ProbeResult{Info: info, State: ProbeUnreachable}, nil
	}
	return ProbeResult{Info: info, State: ProbeReachable, Status: status}, nil
}

// Connect opens a direct connection to exactly the supplied runtime generation.
func Connect(ctx context.Context, identity Identity, info contract.RuntimeInfo) (Client, error) {
	return connectWithDependencies(ctx, identity, info, defaultDependencies())
}

func connectWithDependencies(ctx context.Context, identity Identity, info contract.RuntimeInfo, deps dependencies) (Client, error) {
	if err := identity.validate(); err != nil {
		return nil, err
	}
	if !matchesIdentity(identity, info) {
		return nil, ErrIncompatibleGeneration
	}
	return deps.connect(ctx, info, identity)
}

func matchesIdentity(identity Identity, info contract.RuntimeInfo) bool {
	return identity.ProductVersion == info.ProductVersion && identity.BuildID == info.BuildID
}

func statusFor(identity Identity, deps dependencies, info contract.RuntimeInfo) (*contract.StatusResult, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), deps.callTimeout)
	defer cancel()
	conn, err := deps.connect(ctx, info, identity)
	if err != nil {
		return nil, false
	}
	defer conn.Close()
	response, err := conn.Call(ctx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !response.OK {
		return nil, false
	}
	var status contract.StatusResult
	if err := json.Unmarshal(response.Result, &status); err != nil {
		return nil, false
	}
	if !validStatusFor(identity, info, &status) {
		return nil, false
	}
	return &status, true
}

func validStatusFor(identity Identity, info contract.RuntimeInfo, status *contract.StatusResult) bool {
	if status == nil || status.ProductVersion != identity.ProductVersion || status.BuildID != identity.BuildID ||
		status.PID <= 0 || status.PID != info.PID {
		return false
	}
	switch status.Mode {
	case string(launchcontract.ModeHeadless):
		return status.DesktopOwnerPID == nil
	case string(launchcontract.ModeDesktop):
		return status.DesktopOwnerPID != nil && *status.DesktopOwnerPID > 0
	default:
		return false
	}
}

// GenerationReachable checks the supplied generation directly. It deliberately
// does not reread runtime state, so replacement cannot prove an old generation
// stopped.
func GenerationReachable(identity Identity, info contract.RuntimeInfo) bool {
	return generationReachable(identity, defaultDependencies(), info)
}

func generationReachable(identity Identity, deps dependencies, info contract.RuntimeInfo) bool {
	if identity.validate() != nil || !matchesIdentity(identity, info) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), deps.callTimeout)
	defer cancel()
	conn, err := deps.connect(ctx, info, identity)
	if err != nil {
		return false
	}
	defer conn.Close()
	response, err := conn.Call(ctx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !response.OK {
		return false
	}
	var status contract.StatusResult
	return json.Unmarshal(response.Result, &status) == nil && validStatusFor(identity, info, &status)
}

// EnsureHeadless performs one hot probe, then at most one cold or stale
// headless launch and fixed readiness polling. It does not remove runtime info.
func EnsureHeadless(identity Identity, logLevel string) (StartOutcome, error) {
	return ensure(identity, launchcontract.Headless(), logLevel, defaultDependencies())
}

func ensure(identity Identity, options launchcontract.Options, logLevel string, deps dependencies) (StartOutcome, error) {
	if err := options.Validate(); err != nil {
		return "", err
	}
	result, err := probe(identity, deps)
	if err != nil {
		return "", err
	}
	if result.State == ProbeReachable {
		if !matchesOptions(result.Status, options) {
			return "", ErrModeConflict
		}
		return AlreadyRunning, nil
	}
	if result.State == ProbeIncompatible {
		return "", ErrIncompatibleGeneration
	}
	launched, err := deps.launch(options, logLevel)
	if err != nil {
		return "", wrapSafeOperation("启动 sidraviad", err)
	}
	if err := waitForReadiness(deps.totalWait, deps.pollInterval, launched.exited, func() (bool, error) {
		result, err := probe(identity, deps)
		if err != nil {
			return false, err
		}
		if result.State == ProbeReachable && !matchesOptions(result.Status, options) {
			return false, ErrModeConflict
		}
		return result.State == ProbeReachable && matchesOptions(result.Status, options), nil
	}); err != nil {
		return "", err
	}
	return Started, nil
}

// AcquireHeadless ensures a matching headless daemon and opens one direct IPC client.
func AcquireHeadless(ctx context.Context, identity Identity, logLevel string) (Client, error) {
	return acquire(ctx, identity, launchcontract.Headless(), logLevel, defaultDependencies())
}

// DesktopBootstrapResult is authoritative only after desktop readiness has
// confirmed the exact requested owner and build identity.
type DesktopBootstrapResult struct {
	Info   contract.RuntimeInfo
	Status contract.StatusResult
}

func BootstrapDesktop(identity Identity, ownerPID int) (DesktopBootstrapResult, error) {
	options, err := launchcontract.Desktop(ownerPID)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if !desktopBootstrapSupported() {
		return DesktopBootstrapResult{}, ErrDesktopUnsupported
	}
	return bootstrapDesktop(identity, options, defaultDependencies())
}

func bootstrapDesktop(identity Identity, options launchcontract.Options, deps dependencies) (DesktopBootstrapResult, error) {
	if _, err := ensure(identity, options, "", deps); err != nil {
		return DesktopBootstrapResult{}, err
	}
	result, err := probe(identity, deps)
	if err != nil {
		return DesktopBootstrapResult{}, err
	}
	if result.State != ProbeReachable || !matchesOptions(result.Status, options) ||
		result.Status.ProductVersion != identity.ProductVersion || result.Status.BuildID != identity.BuildID || result.Status.PID <= 0 {
		return DesktopBootstrapResult{}, ErrModeConflict
	}
	return DesktopBootstrapResult{Info: result.Info, Status: *result.Status}, nil
}

// IsReadinessUnconfirmed identifies the stable ambiguous-start error without
// exposing discovery paths or private implementation details.
func IsReadinessUnconfirmed(err error) bool {
	var timeout *daemonReadinessTimeoutError
	return errors.As(err, &timeout)
}

func acquire(ctx context.Context, identity Identity, options launchcontract.Options, logLevel string, deps dependencies) (Client, error) {
	if _, err := ensure(identity, options, logLevel, deps); err != nil {
		return nil, err
	}
	result, err := probe(identity, deps)
	if err != nil {
		return nil, err
	}
	if result.State != ProbeReachable || !matchesOptions(result.Status, options) {
		return nil, fmt.Errorf("无法连接 daemon（未找到运行中的 daemon）")
	}
	connectCtx, cancel := context.WithTimeout(ctx, deps.callTimeout)
	defer cancel()
	return deps.connect(connectCtx, result.Info, identity)
}

func matchesOptions(status *contract.StatusResult, options launchcontract.Options) bool {
	if status == nil || status.Mode != string(options.Mode) {
		return false
	}
	if options.Mode == launchcontract.ModeDesktop {
		return status.DesktopOwnerPID != nil && *status.DesktopOwnerPID == options.DesktopOwnerPID
	}
	return status.DesktopOwnerPID == nil
}

var errDaemonExitedBeforeReadiness = errors.New("sidraviad exited before readiness")

type daemonEarlyExitError struct{ cause error }

func (e *daemonEarlyExitError) Error() string {
	return "sidraviad 在就绪前退出；请检查当前模式的 daemon 日志（portable 包位于 logs\\sidraviad.log）"
}
func (e *daemonEarlyExitError) Unwrap() error { return e.cause }

type daemonReadinessTimeoutError struct{}

func (*daemonReadinessTimeoutError) Error() string {
	return "守护进程：启动结果尚未确认；sidraviad 可能仍在启动。请运行 sidravia daemon status 确认状态后再重试"
}
func (*daemonReadinessTimeoutError) Unwrap() error { return context.DeadlineExceeded }

func waitForReadiness(total, interval time.Duration, exited <-chan error, attempt func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case waitErr := <-exited:
			ready, err := attempt()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			if waitErr == nil {
				waitErr = errDaemonExitedBeforeReadiness
			}
			return &daemonEarlyExitError{cause: waitErr}
		case <-ctx.Done():
			ready, err := attempt()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			return &daemonReadinessTimeoutError{}
		case <-ticker.C:
			ready, err := attempt()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
		}
	}
}
