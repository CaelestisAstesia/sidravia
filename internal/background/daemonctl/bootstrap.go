// Package daemonctl owns discovery and lifecycle coordination for the
// local sidraviad process. CLI presentation deliberately remains outside it.
package daemonctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
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
	ErrDaemonStopped          = errors.New("daemon is not running")
	ErrConnectionUnavailable  = errors.New("unable to connect to daemon")
	ErrIncompatibleGeneration = errors.New("incompatible daemon generation")
	ErrModeConflict           = errors.New("守护进程正在由另一种模式使用；请先退出当前模式")
	ErrDesktopUnsupported     = errors.New("当前平台不支持图形界面 bootstrap")
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

type daemonLaunch struct {
	exited  <-chan error
	pid     int
	alive   func() (bool, error)
	release func()
}

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
	runtimeInfoPath func(productlayout.Namespace) (string, error)
	readRuntimeInfo func(string) (contract.RuntimeInfo, error)
	connect         func(context.Context, contract.RuntimeInfo, Identity) (Client, error)
	launch          func(launchcontract.Options, string) (daemonLaunch, error)
	namespace       func() (productlayout.Namespace, error)
	callTimeout     time.Duration
	totalWait       time.Duration
	pollInterval    time.Duration
}

func defaultDependencies() dependencies {
	return dependencies{
		runtimeInfoPath: func(ns productlayout.Namespace) (string, error) {
			layout, err := productlayout.ResolveNamespace(ns)
			if err != nil {
				return "", err
			}
			return layout.RuntimeInfoPath, nil
		},
		readRuntimeInfo: ReadRuntimeInfo,
		connect: func(ctx context.Context, info contract.RuntimeInfo, identity Identity) (Client, error) {
			return client.Connect(ctx, info.Endpoint, info.Token, identity.BuildID)
		},
		launch: launchDaemonProcess,
		namespace: func() (productlayout.Namespace, error) {
			return productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
		},
		callTimeout:  2 * time.Second,
		totalWait:    ensureTotalWait,
		pollInterval: ensurePollInterval,
	}
}

// EnsureHeadless performs one hot probe, then at most one cold or stale
// headless launch and fixed readiness polling. It does not remove runtime info.
func EnsureHeadless(identity Identity, logLevel string) (StartOutcome, error) {
	deps := defaultDependencies()
	ns, err := deps.namespace()
	if err != nil {
		return "", wrapSafeOperation("运行命名空间", err)
	}
	options := headlessOptions(ns)
	return ensureWithNamespace(identity, options, logLevel, ns, deps)
}

func headlessOptionsWithNamespace(deps dependencies) (launchcontract.Options, error) {
	ns, err := deps.namespace()
	if err != nil {
		return launchcontract.Options{}, wrapSafeOperation("运行命名空间", err)
	}
	return headlessOptions(ns), nil
}

func headlessOptions(ns productlayout.Namespace) launchcontract.Options {
	return launchcontract.Options{Mode: launchcontract.ModeHeadless, Namespace: ns.String()}
}

func ensure(identity Identity, options launchcontract.Options, logLevel string, deps dependencies) (StartOutcome, error) {
	ns, err := deps.namespace()
	if err != nil {
		return "", wrapSafeOperation("运行命名空间", err)
	}
	return ensureWithNamespace(identity, options, logLevel, ns, deps)
}

func ensureWithNamespace(identity Identity, options launchcontract.Options, logLevel string, ns productlayout.Namespace, deps dependencies) (StartOutcome, error) {
	return ensureInContext(context.Background(), identity, options, logLevel, ns, deps)
}

func ensureInContext(ctx context.Context, identity Identity, options launchcontract.Options, logLevel string, ns productlayout.Namespace, deps dependencies) (StartOutcome, error) {
	if err := options.Validate(); err != nil {
		return "", err
	}
	result, err := probeInContext(ctx, identity, ns, deps)
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
	if result.State == ProbeUnreachable && result.Status != nil {
		if result.Status.ProductVersion != identity.ProductVersion || result.Status.BuildID != identity.BuildID {
			return "", ErrIncompatibleGeneration
		}
		return "", ErrModeConflict
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	launched, err := deps.launch(options, logLevel)
	if err != nil {
		return "", wrapSafeOperation("启动 sidraviad", err)
	}
	// A GUI restart can see the old IPC endpoint disappear before its host
	// releases the singleton. Only that policy may relaunch an observed
	// contended child, at most twice inside the original readiness deadline.
	var relaunch func() (daemonLaunch, error)
	if options.Mode == launchcontract.ModeDesktop && options.SkipAutoLogin {
		relaunch = func() (daemonLaunch, error) { return deps.launch(options, logLevel) }
	}
	if err := waitForReadinessInContext(ctx, deps.totalWait, deps.pollInterval, launched.exited, func() (bool, error) {
		result, err := probeInContext(ctx, identity, ns, deps)
		if err != nil {
			return false, err
		}
		if result.State == ProbeIncompatible {
			return false, ErrIncompatibleGeneration
		}
		if result.State == ProbeReachable && !matchesOptions(result.Status, options) {
			return false, ErrModeConflict
		}
		return result.State == ProbeReachable && matchesOptions(result.Status, options), nil
	}, relaunch); err != nil {
		return "", err
	}
	return Started, nil
}

// AcquireHeadless ensures a matching headless daemon and opens one direct IPC client.
// Cancellation stops discovery/readiness waiting; it does not stop a launched daemon.
func AcquireHeadless(ctx context.Context, identity Identity, logLevel string) (Client, error) {
	deps := defaultDependencies()
	ns, err := deps.namespace()
	if err != nil {
		return nil, wrapSafeOperation("运行命名空间", err)
	}
	if layout, registered, err := serviceLayout(ns); registered || err != nil {
		if err != nil {
			return nil, err
		}
		return connectService(ctx, identity, layout)
	}
	options := headlessOptions(ns)
	return acquireWithNamespace(ctx, identity, options, logLevel, ns, deps)
}

// IsReadinessUnconfirmed identifies the stable ambiguous-start error without
// exposing discovery paths or private implementation details.
func IsReadinessUnconfirmed(err error) bool {
	var timeout *daemonReadinessTimeoutError
	return errors.As(err, &timeout)
}

func acquire(ctx context.Context, identity Identity, options launchcontract.Options, logLevel string, deps dependencies) (Client, error) {
	ns, err := deps.namespace()
	if err != nil {
		return nil, wrapSafeOperation("运行命名空间", err)
	}
	return acquireWithNamespace(ctx, identity, options, logLevel, ns, deps)
}

func acquireWithNamespace(ctx context.Context, identity Identity, options launchcontract.Options, logLevel string, ns productlayout.Namespace, deps dependencies) (Client, error) {
	if _, err := ensureInContext(ctx, identity, options, logLevel, ns, deps); err != nil {
		return nil, err
	}
	result, err := probeInContext(ctx, identity, ns, deps)
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
	return MatchesMode(status, options.Mode, options.DesktopOwnerPID)
}

// MatchesMode confirms the reported mode and, for a desktop generation, its owner.
func MatchesMode(status *contract.StatusResult, mode launchcontract.Mode, owner int) bool {
	if status == nil || status.Mode != string(mode) {
		return false
	}
	if mode == launchcontract.ModeDesktop {
		return status.DesktopOwnerPID != nil && *status.DesktopOwnerPID == owner
	}
	return status.DesktopOwnerPID == nil
}

var errDaemonSingletonContention = errors.New("sidraviad singleton contention")

var errDaemonExitedBeforeReadiness = errors.New("sidraviad exited before readiness")

type daemonEarlyExitError struct{ cause error }

func (e *daemonEarlyExitError) Error() string {
	return "sidraviad 在就绪前退出；请检查当前模式的 daemon 日志（portable 包位于 logs\\sidraviad.log）"
}
func (e *daemonEarlyExitError) Unwrap() error { return e.cause }

type daemonReadinessTimeoutError struct{ cause error }

func (*daemonReadinessTimeoutError) Error() string {
	return "守护进程：启动结果尚未确认；sidraviad 可能仍在启动。请运行 sidraviactl daemon status 确认状态后再重试"
}
func (e *daemonReadinessTimeoutError) Unwrap() error {
	return errors.Join(context.DeadlineExceeded, e.cause)
}

func waitForReadiness(total, interval time.Duration, exited <-chan error, attempt func() (bool, error)) error {
	return waitForReadinessWithRelaunch(total, interval, exited, attempt, nil)
}

func waitForReadinessWithRelaunch(total, interval time.Duration, exited <-chan error, attempt func() (bool, error), relaunch func() (daemonLaunch, error)) error {
	return waitForReadinessInContext(context.Background(), total, interval, exited, attempt, relaunch)
}

func waitForReadinessInContext(parent context.Context, total, interval time.Duration, exited <-chan error, attempt func() (bool, error), relaunch func() (daemonLaunch, error)) error {
	ctx, cancel := context.WithTimeout(parent, total)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var contentionCause error
	relaunches := 0
	for {
		if err := parent.Err(); err != nil {
			return err
		}
		select {
		case waitErr := <-exited:
			ready, err := attempt()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			if errors.Is(waitErr, errDaemonSingletonContention) {
				contentionCause = waitErr
				exited = nil // one observed exit must not become a closed-channel busy loop
				continue
			}
			if waitErr == nil {
				waitErr = errDaemonExitedBeforeReadiness
			}
			return &daemonEarlyExitError{cause: waitErr}
		case <-ctx.Done():
			if err := parent.Err(); err != nil {
				return err
			}
			ready, err := attempt()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			return &daemonReadinessTimeoutError{cause: contentionCause}
		case <-ticker.C:
			ready, err := attempt()
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			if exited == nil && contentionCause != nil && relaunch != nil && relaunches < 2 && ctx.Err() == nil {
				child, err := relaunch()
				if err != nil {
					return wrapSafeOperation("启动 sidraviad", err)
				}
				relaunches++
				exited = child.exited
			}
		}
	}
}
