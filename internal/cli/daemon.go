package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/background/serviceswitch"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/ipc/contract"
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

const (
	daemonStatusFailureMessage  = "守护进程：无法确认状态。请运行 sidraviactl daemon status；持续失败时运行 sidraviactl daemon restart"
	daemonStartFailureMessage   = "守护进程：启动失败。请运行 sidraviactl daemon status 确认状态后再重试"
	daemonStopFailureMessage    = "守护进程：停止结果无法确认。请运行 sidraviactl daemon status 确认状态后再重试"
	daemonRestartFailureMessage = "守护进程：重启失败。请运行 sidraviactl daemon status 确认状态后再重试"
	daemonIncompatibleMessage   = "守护进程：当前运行的 daemon 与此 sidraviactl 不属于同一构建。请停止 daemon，或使用与它匹配的完整软件包。"
	daemonReadinessMessage      = "守护进程：启动状态尚未确认。请运行 sidraviactl daemon status 确认后再重试"
)

// daemonCommandError owns the human-readable failure at the public daemon
// command boundary. It retains the original error chain for callers while
// ensuring that neither a wrapped cause nor an arbitrary outer message is
// written to the terminal.
func daemonCommandError(command string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, daemonctl.ErrModeConflict) {
		return terminal.Failure(daemonctl.ErrModeConflict.Error(), err)
	}
	if errors.Is(err, daemonctl.ErrIncompatibleGeneration) {
		return terminal.Failure(daemonIncompatibleMessage, err)
	}
	if daemonctl.IsReadinessUnconfirmed(err) {
		return terminal.Failure(daemonReadinessMessage, err)
	}
	var safe terminal.SafeError
	if errors.As(err, &safe) {
		return terminal.Failure(safe.Error(), err)
	}
	message := daemonStatusFailureMessage
	switch command {
	case "start":
		message = daemonStartFailureMessage
	case "stop":
		message = daemonStopFailureMessage
	case "restart":
		message = daemonRestartFailureMessage
	}
	return terminal.Failure(message, err)
}

type daemonOperations struct {
	inspect func() (daemonctl.ProbeResult, error)
	stop    func(context.Context, contract.RuntimeInfo) error
	start   func(string) (daemonctl.StartOutcome, error)
}

func defaultDaemonOperations(identity daemonctl.Identity) daemonOperations {
	return daemonOperations{
		inspect: func() (daemonctl.ProbeResult, error) { return daemonctl.Probe(identity) },
		stop: func(ctx context.Context, info contract.RuntimeInfo) error {
			return daemonctl.StopGeneration(ctx, identity, info)
		},
		start: func(logLevel string) (daemonctl.StartOutcome, error) {
			return daemonctl.EnsureHeadless(identity, logLevel)
		},
	}
}

// runDaemonStatus remains strictly read-only; bootstrap owns discovery and the
// typed status probe while CLI owns only Chinese terminal.Presentation.
func runDaemonStatus(ops daemonOperations, output io.Writer) error {
	result, err := ops.inspect()
	if err != nil {
		return err
	}
	switch result.State {
	case daemonctl.ProbeStopped:
		return writeDaemonStopped(output)
	case daemonctl.ProbeReachable:
		p := terminal.NewPresentation(output)
		return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonStatus(p, result.Status)))
	case daemonctl.ProbeIncompatible:
		return writeDaemonIncompatible(output)
	default:
		return daemonUnknownError()
	}
}

func daemonUnknownError() error {
	return errors.New("守护进程：无法确认状态（运行信息存在但 daemon 未响应）。请稍后运行 sidraviactl daemon status；持续失败时运行 sidraviactl daemon restart")
}

func writeDaemonIncompatible(w io.Writer) error {
	p := terminal.NewPresentation(w)
	return terminal.Failure("显示 daemon 状态", p.Complete("守护进程：当前运行的 daemon 与此 sidraviactl 不属于同一构建。请停止 daemon，或使用与它匹配的完整软件包。\n"))
}

func writeDaemonStopped(w io.Writer) error {
	p := terminal.NewPresentation(w)
	return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonStopped()))
}

func writeDaemonStarted(w io.Writer) error {
	p := terminal.NewPresentation(w)
	return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonStarted()))
}

func writeDaemonRestarted(w io.Writer) error {
	p := terminal.NewPresentation(w)
	return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonRestarted()))
}

// runDaemonStop sends daemon.stop to the contacted generation and waits until
// exactly that generation is unreachable. It never stops a PID directly.
func runDaemonStop(ops daemonOperations) error {
	_, err := runDaemonStopWithOutcome(ops)
	return err
}

func runDaemonStopWithOutcome(ops daemonOperations) (daemonStopOutcome, error) {
	result, err := ops.inspect()
	if err != nil {
		return "", err
	}
	switch result.State {
	case daemonctl.ProbeStopped:
		return daemonAlreadyStopped, nil
	case daemonctl.ProbeMalformed, daemonctl.ProbeUnreachable:
		return "", daemonUnknownError()
	case daemonctl.ProbeReachable:
		if !isExplicitHeadless(result.Status) {
			return "", daemonctl.ErrModeConflict
		}
		if err := stopGeneration(ops, result.Info); err != nil {
			return "", err
		}
		return daemonStopped, nil
	default:
		return "", daemonUnknownError()
	}
}

func stopGeneration(ops daemonOperations, info contract.RuntimeInfo) error {
	err := ops.stop(context.Background(), info)
	var failure *daemonctl.StopFailure
	if !errors.As(err, &failure) {
		return err
	}
	switch failure.Stage {
	case daemonctl.StopConnect:
		return terminal.Failure("守护进程：停止请求尚未发送，无法确认当前状态；请运行 sidraviactl daemon status 后重试", err)
	case daemonctl.StopCall:
		if failure.CloseCause != nil {
			return fmt.Errorf("%w; %w", terminal.Failure("调用 daemon 停止", failure.Cause), terminal.Failure("清理 sidraviad 连接", failure.CloseCause))
		}
		return terminal.Failure("调用 daemon 停止", err)
	case daemonctl.StopRejected:
		return errors.New(terminal.IPCErrorText(failure.Code))
	case daemonctl.StopUnconfirmed:
		return errors.New("等待 sidraviad 停止超时；停止结果尚未确认。请运行 sidraviactl daemon status 确认状态")
	default:
		return err
	}
}

func runDaemonRestart(ops daemonOperations, logLevel string) error {
	_, err := runDaemonRestartWithOutcome(ops, logLevel)
	return err
}

func runDaemonRestartWithOutcome(ops daemonOperations, logLevel string) (daemonRestartOutcome, error) {
	result, err := ops.inspect()
	if err != nil {
		return "", err
	}
	if result.State == daemonctl.ProbeReachable {
		if !isExplicitHeadless(result.Status) {
			return "", daemonctl.ErrModeConflict
		}
		if err := stopGeneration(ops, result.Info); err != nil {
			return "", err
		}
	}
	if _, err := ops.start(logLevel); err != nil {
		return "", err
	}
	if result.State == daemonctl.ProbeStopped {
		return daemonStartedFromStopped, nil
	}
	return daemonRestarted, nil
}

func isExplicitHeadless(status *contract.StatusResult) bool {
	return status != nil && status.Mode == "headless" && status.DesktopOwnerPID == nil
}

func daemonStart(identity daemonctl.Identity, logLevel string) error {
	if registered, err := daemonctl.HasService(); registered || err != nil {
		if err != nil {
			return err
		}
		// Existing XDG login entries also use daemon start. Keep an
		// automatic login entry from overriding systemd enablement.
		return terminal.Failure("托管后台请使用 sidraviactl integration service start", daemonctl.ErrModeConflict)
	}
	outcome, err := daemonctl.EnsureHeadless(identity, logLevel)
	if err != nil {
		return err
	}
	if outcome == daemonctl.AlreadyRunning {
		p := terminal.NewPresentation(os.Stdout)
		return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonAlreadyRunning()))
	}
	return writeDaemonStarted(os.Stdout)
}

func daemonStatus(identity daemonctl.Identity) error {
	return runDaemonStatus(defaultDaemonOperations(identity), os.Stdout)
}

func daemonStop(identity daemonctl.Identity) error {
	if registered, err := daemonctl.HasService(); registered || err != nil {
		if err != nil {
			return err
		}
		if _, err := serviceswitch.ServiceOperation(context.Background(), identity, "stop"); err != nil {
			return err
		}
		return writeDaemonStopped(os.Stdout)
	}
	outcome, err := runDaemonStopWithOutcome(defaultDaemonOperations(identity))
	if err != nil {
		return err
	}
	if outcome == daemonAlreadyStopped {
		p := terminal.NewPresentation(os.Stdout)
		return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonAlreadyStopped()))
	}
	return writeDaemonStopped(os.Stdout)
}

func daemonRestart(identity daemonctl.Identity, logLevel string) error {
	if registered, err := daemonctl.HasService(); registered || err != nil {
		if err != nil {
			return err
		}
		if _, err := serviceswitch.ServiceOperation(context.Background(), identity, "restart"); err != nil {
			return err
		}
		return writeDaemonRestarted(os.Stdout)
	}
	outcome, err := runDaemonRestartWithOutcome(defaultDaemonOperations(identity), logLevel)
	if err != nil {
		return err
	}
	if outcome == daemonStartedFromStopped {
		p := terminal.NewPresentation(os.Stdout)
		return terminal.Failure("显示 daemon 状态", p.Complete(terminal.RenderDaemonStartedFromStopped()))
	}
	return writeDaemonRestarted(os.Stdout)
}
