package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

const (
	daemonEnsureTotalWait    = 5 * time.Second
	daemonEnsurePollInterval = 200 * time.Millisecond
	daemonStopTotalWait      = 10 * time.Second
	daemonStopPollInterval   = 200 * time.Millisecond
)

func daemonEnvForLevel(parent []string, logLevel string) []string {
	if logLevel == "" {
		logLevel = "info"
	}
	env := make([]string, 0, len(parent)+1)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "SIDRAVIA_LOG_LEVEL") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "SIDRAVIA_LOG_LEVEL="+logLevel)
}

// probeState is the strictly read-only outcome of probing a daemon generation.
type probeState int

const (
	probeStopped     probeState = iota // runtime info is missing
	probeReachable                     // runtime info is valid and daemon.status succeeded
	probeUnreachable                   // runtime info is valid but the generation cannot be reached
	probeMalformed                     // runtime info exists but cannot be decoded
)

type probeResult struct {
	info   contract.RuntimeInfo
	state  probeState
	status *contract.StatusResult
}

type probeDependencies struct {
	runtimeInfoPath func() (string, error)
	readRuntimeInfo func(path string) (contract.RuntimeInfo, error)
	connect         func(context.Context, contract.RuntimeInfo) (daemonClient, error)
	callTimeout     time.Duration
}

func defaultProbeDependencies() probeDependencies {
	return probeDependencies{
		runtimeInfoPath: runtimeInfoPath,
		readRuntimeInfo: readRuntimeInfo,
		connect: func(ctx context.Context, info contract.RuntimeInfo) (daemonClient, error) {
			return client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
		},
		callTimeout: 2 * time.Second,
	}
}

// probeDaemon reads runtime info and attempts a typed daemon.status call. It is
// strictly read-only: it never starts a process, deletes or rewrites runtime
// info, sends a signal, calls stop, or waits for a new generation. Missing
// runtime info is reported as stopped; a valid-but-unreachable generation is
// reported as unreachable; undecodable runtime info is reported as malformed.
func probeDaemon(deps probeDependencies) (probeResult, error) {
	infoPath, err := deps.runtimeInfoPath()
	if err != nil {
		return probeResult{}, wrapSafeOperation("运行信息路径", err)
	}
	info, err := deps.readRuntimeInfo(infoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return probeResult{state: probeStopped}, nil
		}
		return probeResult{state: probeMalformed}, nil
	}
	status, ok := callDaemonStatus(deps, info)
	if !ok {
		return probeResult{info: info, state: probeUnreachable}, nil
	}
	return probeResult{info: info, state: probeReachable, status: status}, nil
}

// callDaemonStatus connects to info and returns the decoded status result. It
// returns ok=false if the generation is unreachable or the response is not OK.
func callDaemonStatus(deps probeDependencies, info contract.RuntimeInfo) (*contract.StatusResult, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), deps.callTimeout)
	defer cancel()
	conn, err := deps.connect(ctx, info)
	if err != nil {
		return nil, false
	}
	defer conn.Close()
	resp, err := conn.Call(ctx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !resp.OK {
		return nil, false
	}
	var status contract.StatusResult
	if err := json.Unmarshal(resp.Result, &status); err != nil {
		return nil, false
	}
	return &status, true
}

// generationReachable reports whether the exact runtime generation described by
// info still answers daemon.status. It connects directly to the generation's
// endpoint, so a momentarily missing runtime file is not mistaken for proof if
// the old generation remains reachable.
func generationReachable(deps probeDependencies, info contract.RuntimeInfo) bool {
	ctx, cancel := context.WithTimeout(context.Background(), deps.callTimeout)
	defer cancel()
	conn, err := deps.connect(ctx, info)
	if err != nil {
		return false
	}
	defer conn.Close()
	resp, err := conn.Call(ctx, contract.MethodDaemonStatus, json.RawMessage("{}"))
	if err != nil || !resp.OK {
		return false
	}
	return true
}

// runDaemonStatus is the strictly read-only daemon status command. Missing
// runtime info prints stopped and exits zero; a reachable daemon prints the
// running status and exits zero; malformed, stale or unreachable runtime
// information reports a fixed safe unknown error and exits nonzero without
// cleaning the file.
func runDaemonStatus(deps probeDependencies, output io.Writer) error {
	result, err := probeDaemon(deps)
	if err != nil {
		return err
	}
	switch result.state {
	case probeStopped:
		return writeDaemonStopped(output)
	case probeReachable:
		p := newPresentation(output)
		return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonStatus(p, result.status)))
	default:
		return errors.New("守护进程：状态未知（unreachable）")
	}
}

func writeDaemonStopped(w io.Writer) error {
	p := newPresentation(w)
	return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonStopped()))
}

func writeDaemonStarted(w io.Writer) error {
	p := newPresentation(w)
	return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonStarted()))
}

func writeDaemonRestarted(w io.Writer) error {
	p := newPresentation(w)
	return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonRestarted()))
}

// ensureDependencies wires ensureDaemonRunning to its collaborators.
type ensureDependencies struct {
	probe        probeDependencies
	launch       func(logLevel string) error
	logLevel     string
	totalWait    time.Duration
	pollInterval time.Duration
}

func defaultEnsureDependencies(logLevel string) ensureDependencies {
	return ensureDependencies{
		probe:        defaultProbeDependencies(),
		launch:       launchDaemonProcess,
		logLevel:     logLevel,
		totalWait:    daemonEnsureTotalWait,
		pollInterval: daemonEnsurePollInterval,
	}
}

// ensureDaemonRunning probes first. A reachable daemon is an idempotent success.
// Otherwise it launches at most once and polls until a typed status succeeds or
// the fixed timeout expires. It does not delete stale runtime information.
func ensureDaemonRunning(deps ensureDependencies) error {
	result, err := probeDaemon(deps.probe)
	if err != nil {
		return err
	}
	if result.state == probeReachable {
		return nil
	}
	if err := deps.launch(deps.logLevel); err != nil {
		return wrapSafeOperation("启动 sidraviad", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), deps.totalWait)
	defer cancel()
	ticker := time.NewTicker(deps.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("等待 sidraviad 超时")
		case <-ticker.C:
			result, err := probeDaemon(deps.probe)
			if err != nil {
				return err
			}
			if result.state == probeReachable {
				return nil
			}
		}
	}
}

// stopDependencies wires runDaemonStop to its collaborators.
type stopDependencies struct {
	probe        probeDependencies
	totalWait    time.Duration
	pollInterval time.Duration
}

func defaultStopDependencies() stopDependencies {
	return stopDependencies{
		probe:        defaultProbeDependencies(),
		totalWait:    daemonStopTotalWait,
		pollInterval: daemonStopPollInterval,
	}
}

// runDaemonStop sends daemon.stop to the contacted generation and waits until
// that exact generation is no longer reachable. It is idempotent when no
// reachable daemon exists. It never terminates by PID.
func runDaemonStop(deps stopDependencies) error {
	result, err := probeDaemon(deps.probe)
	if err != nil {
		return err
	}
	switch result.state {
	case probeStopped:
		return nil
	case probeMalformed, probeUnreachable:
		return errors.New("守护进程：状态未知（unreachable）")
	case probeReachable:
		return stopGeneration(deps, result.info)
	default:
		return errors.New("守护进程：状态未知（unreachable）")
	}
}

func stopGeneration(deps stopDependencies, info contract.RuntimeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), deps.probe.callTimeout)
	defer cancel()
	conn, err := deps.probe.connect(ctx, info)
	if err != nil {
		return nil
	}
	resp, err := conn.Call(ctx, contract.MethodDaemonStop, json.RawMessage("{}"))
	closeErr := conn.Close()
	if err != nil {
		return wrapSafeOperation("调用 daemon 停止", err)
	}
	if closeErr != nil {
		return wrapSafeOperation("关闭 sidraviad 连接", closeErr)
	}
	if !resp.OK {
		code := ""
		if resp.Error != nil {
			code = resp.Error.Code
		}
		return errors.New(ipcErrorText(code))
	}
	return waitForGenerationUnreachable(deps.probe, info, deps.totalWait, deps.pollInterval)
}

func waitForGenerationUnreachable(deps probeDependencies, info contract.RuntimeInfo, totalWait, pollInterval time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), totalWait)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("等待 sidraviad 停止超时")
		case <-ticker.C:
			if !generationReachable(deps, info) {
				return nil
			}
		}
	}
}

// restartDependencies wires runDaemonRestart to its collaborators.
type restartDependencies struct {
	probe  probeDependencies
	stop   stopDependencies
	ensure ensureDependencies
}

func defaultRestartDependencies(logLevel string) restartDependencies {
	probe := defaultProbeDependencies()
	return restartDependencies{
		probe: probe,
		stop:  defaultStopDependencies(),
		ensure: ensureDependencies{
			probe:        probe,
			launch:       launchDaemonProcess,
			logLevel:     logLevel,
			totalWait:    daemonEnsureTotalWait,
			pollInterval: daemonEnsurePollInterval,
		},
	}
}

// runDaemonRestart handles stopped, running and stale cases. If stopped, it is
// start. If running, it sends stop, waits for the old generation to become
// unreachable, then ensures a new daemon is running. If stale or malformed
// runtime info exists, it does not delete it; it attempts a normal start and
// lets the daemon host own atomic replacement.
func runDaemonRestart(deps restartDependencies) error {
	result, err := probeDaemon(deps.probe)
	if err != nil {
		return err
	}
	if result.state == probeReachable {
		if err := stopGeneration(deps.stop, result.info); err != nil {
			return err
		}
	}
	return ensureDaemonRunning(deps.ensure)
}

// daemonStart, daemonStop and daemonRestart are the production entry points
// bound to the CLI command tree.
func daemonStart(logLevel string) error {
	if err := ensureDaemonRunning(defaultEnsureDependencies(logLevel)); err != nil {
		return err
	}
	return writeDaemonStarted(os.Stdout)
}

func daemonStatus() error {
	return runDaemonStatus(defaultProbeDependencies(), os.Stdout)
}

func daemonStop() error {
	if err := runDaemonStop(defaultStopDependencies()); err != nil {
		return err
	}
	return writeDaemonStopped(os.Stdout)
}

func daemonRestart(logLevel string) error {
	if err := runDaemonRestart(defaultRestartDependencies(logLevel)); err != nil {
		return err
	}
	return writeDaemonRestarted(os.Stdout)
}
