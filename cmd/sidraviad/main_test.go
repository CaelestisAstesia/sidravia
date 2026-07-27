package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// fakeRuntime is a test double for runtimeLike. It returns a preset error (or
// nil) from run so the process boundary can be exercised without constructing
// the full production object graph.
type fakeRuntime struct {
	runErr error
}

func (f *fakeRuntime) run(_ context.Context) error { return f.runErr }

// TestNewDaemonLoggerIsTextInfo proves the production handler is a TextHandler
// at Info level that records the fixed Simplified Chinese message, the stable
// event code and only the permitted attributes.
func TestNewDaemonLoggerIsTextInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := newDaemonLogger(&buf)

	logger.Debug("debug must be dropped", slog.String("event", "debug_event"))
	logger.Info(msgDaemonRuntimeStarted,
		slog.String("event", eventDaemonRuntimeStarted),
		slog.String("product_version", "1.0.0-test"),
		slog.String("build_id", "abc1234"),
		slog.Int("pid", 4242),
	)

	output := buf.String()
	if strings.Contains(output, "debug must be dropped") {
		t.Fatalf("Debug record was not dropped at Info level, got:\n%s", output)
	}
	// TextHandler emits space-separated key=value pairs; a JSONHandler would
	// emit a JSON object instead.
	if !strings.Contains(output, "level=INFO") {
		t.Fatalf("expected TextHandler level=INFO, got:\n%s", output)
	}
	if !strings.Contains(output, "msg=守护进程运行已启动") {
		t.Fatalf("expected fixed Simplified Chinese message, got:\n%s", output)
	}
	if !strings.Contains(output, "event=daemon_runtime_started") {
		t.Fatalf("expected stable event code, got:\n%s", output)
	}
	if !strings.Contains(output, "product_version=1.0.0-test") {
		t.Fatalf("expected permitted product_version attribute, got:\n%s", output)
	}
	if !strings.Contains(output, "build_id=abc1234") {
		t.Fatalf("expected permitted build_id attribute, got:\n%s", output)
	}
	if !strings.Contains(output, "pid=4242") {
		t.Fatalf("expected permitted pid attribute, got:\n%s", output)
	}
}

// TestRunMainConstructionFailureEmitsStartFailed proves a construction failure
// emits exactly one daemon_start_failed event, returns exit code 1, never
// includes the returned error or cause, and does not also emit a runtime event.
func TestRunMainConstructionFailureEmitsStartFailed(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	secretMarker := "SECRET-DIAGNOSTIC-7F4E2A91"

	construct := systemConstructor(func(_ context.Context, _ *slog.Logger) (runtimeLike, error) {
		return nil, fmt.Errorf("construction failed: password=%s token=%s", secretMarker, secretMarker)
	})

	code := runMainWith(logger, construct)
	if code != 1 {
		t.Fatalf("runMain exit = %d, want 1 on construction failure", code)
	}

	output := buf.String()
	if strings.Count(output, "event=daemon_start_failed") != 1 {
		t.Fatalf("expected exactly one daemon_start_failed, got:\n%s", output)
	}
	for _, event := range []string{
		eventDaemonRuntimeStarted,
		eventDaemonRuntimeFailed,
		eventDaemonRuntimeStopped,
		eventNetworkSnapshotApplied,
	} {
		if strings.Contains(output, "event="+event) {
			t.Fatalf("construction failure must not emit %s, got:\n%s", event, output)
		}
	}
	if strings.Contains(output, secretMarker) {
		t.Fatalf("secret diagnostic leaked into construction failure log, got:\n%s", output)
	}
	if strings.Contains(output, "error=") {
		t.Fatalf("fatal log must not carry an error attribute, got:\n%s", output)
	}
}

// TestRunMainRuntimeFailureEmitsRuntimeFailed proves a runtime failure emits
// exactly one daemon_runtime_failed event, returns exit code 1, never includes
// the returned error or cause, and does not also emit a start_failed event.
func TestRunMainRuntimeFailureEmitsRuntimeFailed(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	secretMarker := "SECRET-RUNTIME-3B8C1D52"

	construct := systemConstructor(func(_ context.Context, _ *slog.Logger) (runtimeLike, error) {
		return &fakeRuntime{runErr: fmt.Errorf("runtime failed: token=%s", secretMarker)}, nil
	})

	code := runMainWith(logger, construct)
	if code != 1 {
		t.Fatalf("runMain exit = %d, want 1 on runtime failure", code)
	}

	output := buf.String()
	if strings.Count(output, "event=daemon_runtime_failed") != 1 {
		t.Fatalf("expected exactly one daemon_runtime_failed, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_start_failed") {
		t.Fatalf("runtime failure must not emit daemon_start_failed, got:\n%s", output)
	}
	if strings.Contains(output, secretMarker) {
		t.Fatalf("secret runtime cause leaked into fatal log, got:\n%s", output)
	}
}

// TestRunMainNormalReturnsZero proves a normal runtime returns exit code 0 and
// emits no process-boundary failure event.
func TestRunMainNormalReturnsZero(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	construct := systemConstructor(func(_ context.Context, _ *slog.Logger) (runtimeLike, error) {
		return &fakeRuntime{runErr: nil}, nil
	})

	code := runMainWith(logger, construct)
	if code != 0 {
		t.Fatalf("runMain exit = %d, want 0 on normal completion", code)
	}

	output := buf.String()
	for _, event := range []string{eventDaemonStartFailed, eventDaemonRuntimeFailed} {
		if strings.Contains(output, "event="+event) {
			t.Fatalf("normal completion must not emit %s, got:\n%s", event, output)
		}
	}
}
