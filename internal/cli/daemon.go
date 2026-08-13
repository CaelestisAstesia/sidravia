package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

const (
	daemonStopTotalWait    = 10 * time.Second
	daemonStopPollInterval = 200 * time.Millisecond
)

type daemonStopOutcome string

const (
	daemonStopped        daemonStopOutcome = "stopped"
	daemonAlreadyStopped daemonStopOutcome = "already_stopped"
)

type daemonRestartOutcome string

const (
	daemonRestarted          daemonRestartOutcome = "restarted"
	daemonStartedFromStopped daemonRestartOutcome = "started_from_stopped"
)

type daemonOperations struct {
	inspect   func() (clientbootstrap.ProbeResult, error)
	connect   func(context.Context, contract.RuntimeInfo) (daemonClient, error)
	reachable func(contract.RuntimeInfo) bool
	start     func(string) (clientbootstrap.StartOutcome, error)
}

func defaultDaemonOperations() daemonOperations {
	return daemonOperations{
		inspect: clientbootstrap.Probe,
		connect: func(ctx context.Context, info contract.RuntimeInfo) (daemonClient, error) {
			return clientbootstrap.Connect(ctx, info)
		},
		reachable: clientbootstrap.GenerationReachable,
		start:     clientbootstrap.Ensure,
	}
}

// runDaemonStatus remains strictly read-only; bootstrap owns discovery and the
// typed status probe while CLI owns only Chinese presentation.
func runDaemonStatus(ops daemonOperations, output io.Writer) error {
	result, err := ops.inspect()
	if err != nil {
		return err
	}
	switch result.State {
	case clientbootstrap.ProbeStopped:
		return writeDaemonStopped(output)
	case clientbootstrap.ProbeReachable:
		p := newPresentation(output)
		return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonStatus(p, result.Status)))
	default:
		return daemonUnknownError()
	}
}

func daemonUnknownError() error {
	return errors.New("守护进程：无法确认状态（运行信息存在但 daemon 未响应）。请稍后运行 sidravia daemon status；持续失败时运行 sidravia daemon restart")
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

type stopDependencies struct {
	ops          daemonOperations
	totalWait    time.Duration
	pollInterval time.Duration
}

func defaultStopDependencies() stopDependencies {
	return stopDependencies{ops: defaultDaemonOperations(), totalWait: daemonStopTotalWait, pollInterval: daemonStopPollInterval}
}

// runDaemonStop sends daemon.stop to the contacted generation and waits until
// exactly that generation is unreachable. It never stops a PID directly.
func runDaemonStop(deps stopDependencies) error {
	_, err := runDaemonStopWithOutcome(deps)
	return err
}

func runDaemonStopWithOutcome(deps stopDependencies) (daemonStopOutcome, error) {
	result, err := deps.ops.inspect()
	if err != nil {
		return "", err
	}
	switch result.State {
	case clientbootstrap.ProbeStopped:
		return daemonAlreadyStopped, nil
	case clientbootstrap.ProbeMalformed, clientbootstrap.ProbeUnreachable:
		return "", daemonUnknownError()
	case clientbootstrap.ProbeReachable:
		if err := stopGeneration(deps, result.Info); err != nil {
			return "", err
		}
		return daemonStopped, nil
	default:
		return "", daemonUnknownError()
	}
}

func stopGeneration(deps stopDependencies, info contract.RuntimeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := deps.ops.connect(ctx, info)
	if err != nil {
		return wrapSafeOperation("守护进程：停止请求尚未发送，无法确认当前状态；请运行 sidravia daemon status 后重试", err)
	}
	response, err := conn.Call(ctx, contract.MethodDaemonStop, json.RawMessage("{}"))
	closeErr := conn.Close()
	if err != nil {
		if closeErr != nil {
			return fmt.Errorf("%w; %w", wrapSafeOperation("调用 daemon 停止", err), wrapSafeOperation("清理 sidraviad 连接", closeErr))
		}
		return wrapSafeOperation("调用 daemon 停止", err)
	}
	_ = closeErr
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		return errors.New(ipcErrorText(code))
	}
	return waitForStoppedGeneration(deps, info)
}

func waitForStoppedGeneration(deps stopDependencies, info contract.RuntimeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), deps.totalWait)
	defer cancel()
	ticker := time.NewTicker(deps.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("等待 sidraviad 停止超时；停止结果尚未确认。请运行 sidravia daemon status 确认状态")
		case <-ticker.C:
			if !deps.ops.reachable(info) {
				return nil
			}
		}
	}
}

type restartDependencies struct {
	ops  daemonOperations
	stop stopDependencies
}

func defaultRestartDependencies() restartDependencies {
	ops := defaultDaemonOperations()
	return restartDependencies{ops: ops, stop: stopDependencies{ops: ops, totalWait: daemonStopTotalWait, pollInterval: daemonStopPollInterval}}
}

func runDaemonRestart(deps restartDependencies, logLevel string) error {
	_, err := runDaemonRestartWithOutcome(deps, logLevel)
	return err
}

func runDaemonRestartWithOutcome(deps restartDependencies, logLevel string) (daemonRestartOutcome, error) {
	result, err := deps.ops.inspect()
	if err != nil {
		return "", err
	}
	if result.State == clientbootstrap.ProbeReachable {
		if err := stopGeneration(deps.stop, result.Info); err != nil {
			return "", err
		}
	}
	if _, err := deps.ops.start(logLevel); err != nil {
		return "", err
	}
	if result.State == clientbootstrap.ProbeStopped {
		return daemonStartedFromStopped, nil
	}
	return daemonRestarted, nil
}

func daemonStart(logLevel string) error {
	outcome, err := clientbootstrap.Ensure(logLevel)
	if err != nil {
		return err
	}
	if outcome == clientbootstrap.AlreadyRunning {
		p := newPresentation(os.Stdout)
		return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonAlreadyRunning()))
	}
	return writeDaemonStarted(os.Stdout)
}

func daemonStatus() error { return runDaemonStatus(defaultDaemonOperations(), os.Stdout) }

func daemonStop() error {
	outcome, err := runDaemonStopWithOutcome(defaultStopDependencies())
	if err != nil {
		return err
	}
	if outcome == daemonAlreadyStopped {
		p := newPresentation(os.Stdout)
		return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonAlreadyStopped()))
	}
	return writeDaemonStopped(os.Stdout)
}

func daemonRestart(logLevel string) error {
	outcome, err := runDaemonRestartWithOutcome(defaultRestartDependencies(), logLevel)
	if err != nil {
		return err
	}
	if outcome == daemonStartedFromStopped {
		p := newPresentation(os.Stdout)
		return wrapSafeOperation("显示 daemon 状态", p.complete(renderDaemonStartedFromStopped()))
	}
	return writeDaemonRestarted(os.Stdout)
}
