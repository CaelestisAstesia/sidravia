// Package clientbootstrap owns discovery and lifecycle coordination for the
// local sidraviad process.  CLI presentation deliberately remains outside it.
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
	"sidravia/internal/productlayout"
)

const (
	ensureTotalWait    = 5 * time.Second
	ensurePollInterval = 200 * time.Millisecond
	stopTotalWait      = 10 * time.Second
	stopPollInterval   = 200 * time.Millisecond
)

// Client is the daemon connection consumed by CLI operations.
type Client interface {
	Call(context.Context, string, json.RawMessage) (contract.Response, error)
	Close() error
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
	return &safeOperationError{label, cause}
}

func daemonEnvForLevel(parent []string, logLevel string) []string {
	if logLevel == "" {
		logLevel = "info"
	}
	env := make([]string, 0, len(parent)+1)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "SIDRAVIA_LOG_LEVEL") {
			env = append(env, entry)
		}
	}
	return append(env, "SIDRAVIA_LOG_LEVEL="+logLevel)
}

type ProbeState int

const (
	ProbeStopped ProbeState = iota
	ProbeReachable
	ProbeUnreachable
	ProbeMalformed
)

type ProbeResult struct {
	Info   contract.RuntimeInfo
	State  ProbeState
	Status *contract.StatusResult
}

func runtimeInfoPath() (string, error) {
	layout, err := productlayout.Resolve()
	if err != nil {
		return "", err
	}
	return layout.RuntimeInfoPath, nil
}
func readRuntimeInfo(path string) (contract.RuntimeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	return contract.DecodeRuntimeInfo(data)
}

// Probe is read-only and never starts, stops, removes or rewrites runtime state.
func Probe() (ProbeResult, error) {
	path, err := runtimeInfoPath()
	if err != nil {
		return ProbeResult{}, wrapSafeOperation("运行信息路径", err)
	}
	info, err := readRuntimeInfo(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProbeResult{State: ProbeStopped}, nil
		}
		return ProbeResult{State: ProbeMalformed}, nil
	}
	status, ok := callDaemonStatus(info)
	if !ok {
		return ProbeResult{Info: info, State: ProbeUnreachable}, nil
	}
	return ProbeResult{Info: info, State: ProbeReachable, Status: status}, nil
}

func connect(ctx context.Context, info contract.RuntimeInfo) (Client, error) {
	return client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
}
func callDaemonStatus(info contract.RuntimeInfo) (*contract.StatusResult, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := connect(ctx, info)
	if err != nil {
		return nil, false
	}
	defer c.Close()
	resp, err := c.Call(ctx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !resp.OK {
		return nil, false
	}
	var status contract.StatusResult
	if json.Unmarshal(resp.Result, &status) != nil {
		return nil, false
	}
	return &status, true
}

type StartOutcome string

const (
	Started        StartOutcome = "started"
	AlreadyRunning StartOutcome = "already_running"
)

func Ensure(logLevel string) (StartOutcome, error) {
	result, err := Probe()
	if err != nil {
		return "", err
	}
	if result.State == ProbeReachable {
		return AlreadyRunning, nil
	}
	launch, err := launchDaemonProcess(logLevel)
	if err != nil {
		return "", wrapSafeOperation("启动 sidraviad", err)
	}
	if err := waitForReadiness(ensureTotalWait, ensurePollInterval, launch.exited, func() bool { result, err := Probe(); return err == nil && result.State == ProbeReachable }); err != nil {
		return "", err
	}
	return Started, nil
}

// Launch starts exactly one sibling daemon process. Readiness remains the
// caller's responsibility; Ensure is the normal production entry point.
func Launch(logLevel string) (<-chan error, error) {
	launch, err := launchDaemonProcess(logLevel)
	if err != nil {
		return nil, err
	}
	return launch.exited, nil
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
func waitForReadiness(total, interval time.Duration, exited <-chan error, ready func() bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case waitErr := <-exited:
			if ready() {
				return nil
			}
			if waitErr == nil {
				waitErr = errDaemonExitedBeforeReadiness
			}
			return &daemonEarlyExitError{waitErr}
		case <-ctx.Done():
			if ready() {
				return nil
			}
			return &daemonReadinessTimeoutError{}
		case <-ticker.C:
			if ready() {
				return nil
			}
		}
	}
}

// ConnectHeadless ensures a daemon then opens one direct IPC connection.
func ConnectHeadless(ctx context.Context) (Client, error) {
	if _, err := Ensure(""); err != nil {
		return nil, err
	}
	result, err := Probe()
	if err != nil {
		return nil, err
	}
	if result.State != ProbeReachable {
		return nil, fmt.Errorf("daemon unavailable")
	}
	return connect(ctx, result.Info)
}
