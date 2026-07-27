package main

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Set via ldflags.
var (
	ProductVersion = "0.1.0-dev"
	BuildID        = "dev"
)

func main() {
	os.Exit(runMain(newProductionLogger()))
}

// runtimeLike is the subset of *composedRuntime that the process boundary
// needs. It is a private seam so tests can inject a runtime that fails without
// constructing the full production object graph. It does not enlarge any public
// surface: main calls runMain, which calls constructProductionSystem.
type runtimeLike interface {
	run(context.Context) error
}

// systemConstructor builds the production runtime. It is a private seam for the
// same reason as runtimeLike.
type systemConstructor func(context.Context, *slog.Logger) (runtimeLike, error)

// runMain is the testable process entry point. It constructs the production
// system, runs it, and returns the exit code. Fatal reporting is split from
// os.Exit so tests can assert events without terminating the process.
func runMain(logger *slog.Logger) int {
	construct := systemConstructor(func(ctx context.Context, l *slog.Logger) (runtimeLike, error) {
		return constructProductionSystem(ctx, l)
	})
	return runMainWith(logger, construct)
}

// runMainWith is the process boundary shared by production and tests. Each
// failure path emits exactly one event and never includes the returned error or
// cause; logging does not replace error propagation, so the original error
// stays with the caller for ownership and tests.
func runMainWith(logger *slog.Logger, construct systemConstructor) int {
	rt, err := construct(context.Background(), logger)
	if err != nil {
		reportFatal(logger, eventDaemonStartFailed, msgDaemonStartFailed)
		return 1
	}
	if err := rt.run(context.Background()); err != nil {
		reportFatal(logger, eventDaemonRuntimeFailed, msgDaemonRuntimeFailed)
		return 1
	}
	return 0
}

// newProductionLogger constructs the single daemon-wide structured logger. It
// writes TextHandler records to stderr at Info level. It must not mutate the
// package-global default logger.
func newProductionLogger() *slog.Logger {
	return newDaemonLogger(os.Stderr)
}

// newDaemonLogger builds a TextHandler logger at Info level over w. It is
// separated from newProductionLogger so tests can assert the handler shape and
// level without capturing process stderr.
func newDaemonLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// reportFatal emits exactly one process-boundary failure event. It never
// includes the returned error or wrapped cause.
func reportFatal(logger *slog.Logger, event, msg string) {
	logger.Error(msg, slog.String("event", event))
}
