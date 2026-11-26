package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"sidravia/internal/connection/environment"
)

type runtimeActivity uint8

const (
	runtimeActivityHost runtimeActivity = iota
	runtimeActivityObserver
	runtimeActivityDelivery
	runtimeActivityDesktopOwner
)

type runtimeActivityResult struct {
	activity runtimeActivity
	err      error
}

func (rt *composedRuntime) run(ctx context.Context) error {
	if ctx == nil {
		// A nil context is a programming error before runtime start. The runtime
		// never started, so it emits neither a started nor a stopped event; the
		// caller owns the single daemon_runtime_failed record.
		return rt.closeAndWait(errors.New("sidraviad: nil context"))
	}
	if err := ctx.Err(); err != nil {
		return rt.closeAndWait(nil)
	}

	rt.logger.Info(msgDaemonRuntimeStarted,
		slog.String("event", eventDaemonRuntimeStarted),
		slog.String("product_version", rt.productVersion),
		slog.String("build_id", rt.buildID),
		slog.Int("pid", os.Getpid()),
	)

	childCtx, cancel := context.WithCancel(ctx)
	var traceWorker sync.WaitGroup
	if rt.networkTrace != nil {
		traceWorker.Add(1)
		go func() { defer traceWorker.Done(); rt.networkTrace.run(childCtx) }()
	}
	defer func() { cancel(); traceWorker.Wait() }()

	hostReady := make(chan struct{})
	var readyOnce sync.Once
	hostCfg := rt.hostCfg
	hostCfg.OnReady = func() { readyOnce.Do(func() { close(hostReady) }) }
	snapshotCh := make(chan environment.Snapshot, 1)
	activityCount := 3
	if rt.desktopOwner != nil {
		activityCount++
	}
	results := make(chan runtimeActivityResult, activityCount)

	go func() {
		results <- runtimeActivityResult{
			activity: runtimeActivityHost,
			err:      rt.hostRunner(childCtx, hostCfg),
		}
	}()

	if rt.desktopOwner != nil {
		go func() {
			results <- runtimeActivityResult{activity: runtimeActivityDesktopOwner, err: rt.desktopOwner.Wait(childCtx)}
		}()
	}

	go func() {
		results <- runtimeActivityResult{
			activity: runtimeActivityObserver,
			err:      rt.observer.Observe(childCtx, snapshotCh),
		}
	}()

	go func() {
		results <- runtimeActivityResult{
			activity: runtimeActivityDelivery,
			err:      rt.deliverSnapshots(childCtx, snapshotCh, hostReady),
		}
	}()

	var initiator error
	received := 0
	inspectRemaining := false
	select {
	case <-ctx.Done():
		inspectRemaining = true
	case <-rt.stopCh:
		// A committed daemon.stop response has been written to the client. Begin
		// graceful shutdown: cancel the child context so host/observer/delivery
		// exit, then close and wait for Supervisor through the existing path.
		rt.logger.Info(msgDaemonStopRequested, slog.String("event", eventDaemonStopRequested))
		inspectRemaining = false
	case result := <-results:
		received = 1
		initiator = classifyRuntimeResult(ctx, result)
		inspectRemaining = ctx.Err() != nil
	}

	cancel()

	var ipcResult <-chan error
	if rt.ipcShutdown != nil {
		result := make(chan error, 1)
		ipcCtx, ipcCancel := context.WithTimeout(context.Background(), 5*time.Second)
		go func() {
			defer ipcCancel()
			result <- rt.ipcShutdown.Shutdown(ipcCtx)
		}()
		ipcResult = result
	}

	for received < activityCount {
		result := <-results
		received++
		if inspectRemaining && initiator == nil {
			initiator = classifyRuntimeResult(ctx, result)
		}
	}

	traceWorker.Wait()
	finalErr := rt.closeAndWait(initiator)
	if rt.desktopOwner != nil {
		if closeErr := rt.desktopOwner.Close(); closeErr != nil {
			wrapped := fmt.Errorf("sidraviad: close desktop owner: %w", closeErr)
			if finalErr != nil {
				finalErr = errors.Join(finalErr, wrapped)
			} else {
				finalErr = wrapped
			}
		}
	}
	if ipcResult != nil {
		if ipcErr := <-ipcResult; ipcErr != nil {
			wrapped := fmt.Errorf("sidraviad: IPC shutdown: %w", ipcErr)
			if finalErr != nil {
				finalErr = errors.Join(finalErr, wrapped)
			} else {
				finalErr = wrapped
			}
		}
	}
	if finalErr == nil {
		rt.logger.Info(msgDaemonRuntimeStopped, slog.String("event", eventDaemonRuntimeStopped))
	}
	// A failed runtime does not also log the normal stopped event. The single
	// daemon_runtime_failed record is owned by the process boundary (runMain) so
	// the original cause stays out of the log while error propagation is kept.
	return finalErr
}

func classifyRuntimeResult(ctx context.Context, result runtimeActivityResult) error {
	if callerErr := ctx.Err(); callerErr != nil &&
		(result.err == nil || errors.Is(result.err, callerErr)) {
		return nil
	}

	switch result.activity {
	case runtimeActivityHost:
		if result.err == nil {
			return nil
		}
		return fmt.Errorf("sidraviad: host: %w", result.err)
	case runtimeActivityObserver:
		if result.err == nil {
			return errors.New("sidraviad: observer stopped unexpectedly")
		}
		return fmt.Errorf("sidraviad: observer: %w", result.err)
	case runtimeActivityDelivery:
		if result.err == nil {
			return errors.New("sidraviad: snapshot delivery stopped unexpectedly")
		}
		return fmt.Errorf("sidraviad: snapshot delivery: %w", result.err)
	case runtimeActivityDesktopOwner:
		if result.err == nil {
			return nil
		}
		return fmt.Errorf("sidraviad: desktop owner: %w", result.err)
	default:
		return errors.New("sidraviad: unknown runtime activity stopped")
	}
}

func (rt *composedRuntime) closeAndWait(initiator error) error {
	closeErr := rt.shutdown.Close()
	rt.shutdown.Wait()
	if closeErr == nil {
		return initiator
	}
	wrapped := fmt.Errorf("sidraviad: supervisor close: %w", closeErr)
	if initiator != nil {
		return errors.Join(initiator, wrapped)
	}
	return wrapped
}

func (rt *composedRuntime) deliverSnapshots(ctx context.Context, snapshotCh <-chan environment.Snapshot, hostReady <-chan struct{}) error {
	automaticLoginPerformed := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case snapshot, ok := <-snapshotCh:
			if !ok {
				return nil
			}
			if err := rt.snapshotSink.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("deliver snapshot: %w", err)
			}
			rt.logger.Info(msgNetworkSnapshotApplied,
				slog.String("event", eventNetworkSnapshotApplied),
				slog.Uint64("revision", snapshot.Revision),
				slog.Int("interface_count", len(snapshot.Interfaces())),
			)
			if !automaticLoginPerformed && !rt.launchOptions.SkipAutoLogin {
				select {
				case <-hostReady:
				case <-ctx.Done():
					return nil
				}
				if ctx.Err() != nil {
					return nil
				}
				automaticLoginPerformed = true
				if err := rt.automaticLogin.PerformAutomaticLogin(ctx); err != nil {
					rt.logger.Warn(msgAutomaticLoginFailed,
						slog.String("event", eventAutomaticLoginFailed),
					)
				}
			}
		}
	}
}
