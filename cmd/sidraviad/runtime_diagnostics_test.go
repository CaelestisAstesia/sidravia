package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

// TestRuntimeLogsStartSnapshotStop proves the runtime emits daemon_runtime_started
// with product_version/build_id/pid, network_snapshot_applied with revision and
// interface_count, and daemon_runtime_stopped on normal completion.
func TestRuntimeLogsStartSnapshotStop(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	observer := newFakeObserver()
	observer.block = true
	observer.addSnapshot(environment.NewSnapshot(7, time.Unix(100, 0), nil))
	hostRunner := newFakeHostRunner()
	sink := newFakeSnapshotSink()
	shutdown := &fakeShutdown{
		activityDone: []<-chan struct{}{hostRunner.finished, observer.done, sink.done},
	}
	rt := newLifecycleRuntime(observer, hostRunner, sink, shutdown, logger)

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, sink.started, "snapshot delivery")
	hostRunner.signalDone(nil)

	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("run() error = %v, want nil on normal completion", err)
	}

	output := buf.String()
	if strings.Count(output, "event=daemon_runtime_started") != 1 {
		t.Fatalf("expected one daemon_runtime_started, got:\n%s", output)
	}
	if !strings.Contains(output, "product_version=test-version") {
		t.Fatalf("expected product_version=test-version, got:\n%s", output)
	}
	if !strings.Contains(output, "build_id=test-build") {
		t.Fatalf("expected build_id=test-build, got:\n%s", output)
	}
	if !strings.Contains(output, "pid=") {
		t.Fatalf("expected pid attribute, got:\n%s", output)
	}
	if strings.Count(output, "event=network_snapshot_applied") != 1 {
		t.Fatalf("expected one network_snapshot_applied, got:\n%s", output)
	}
	if !strings.Contains(output, "revision=7") {
		t.Fatalf("expected revision=7, got:\n%s", output)
	}
	if !strings.Contains(output, "interface_count=0") {
		t.Fatalf("expected interface_count=0, got:\n%s", output)
	}
	if strings.Count(output, "event=daemon_runtime_stopped") != 1 {
		t.Fatalf("expected one daemon_runtime_stopped, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_runtime_failed") {
		t.Fatalf("normal runtime must not log daemon_runtime_failed, got:\n%s", output)
	}
}

// TestRuntimeFailureLogsStartedWithoutStopOrCause proves a failed runtime logs
// daemon_runtime_started, never logs daemon_runtime_stopped, and never leaks the
// original cause. The single daemon_runtime_failed record is owned by the process
// boundary, not by run.
func TestRuntimeFailureLogsStartedWithoutStopOrCause(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	rt, observer, hostRunner, sink, _ := newCoordinatedLifecycle(t, logger)
	observerErr := errors.New("observer failure sentinel-secret-9Z4E1B")
	observer.block = false
	observer.err = observerErr
	observer.afterPublish = sink.started

	result := runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	err := waitForRuntimeResult(t, result)
	if !errors.Is(err, observerErr) {
		t.Fatalf("run() error = %v, want observer sentinel in error chain", err)
	}

	output := buf.String()
	if strings.Count(output, "event=daemon_runtime_started") != 1 {
		t.Fatalf("expected one daemon_runtime_started, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_runtime_stopped") {
		t.Fatalf("failed runtime must not log daemon_runtime_stopped, got:\n%s", output)
	}
	if strings.Contains(output, "event=daemon_runtime_failed") {
		t.Fatalf("run must not own the daemon_runtime_failed record, got:\n%s", output)
	}
	if strings.Contains(output, "sentinel-secret-9Z4E1B") {
		t.Fatalf("original cause leaked into runtime log, got:\n%s", output)
	}
}

// TestCompositionInjectsSessionDiagnostics proves the composition root wires the
// production session diagnostics adapter into created Sessions, so the Info
// session_snapshot event appears and the password never does.
func TestCompositionInjectsSessionDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	rt := composeRuntimeWithJLUProfile(t, bufferLogger(&buf)).runtime
	defer func() {
		rt.shutdown.Close()
		rt.shutdown.Wait()
	}()
	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	ctx := context.Background()
	conn, err := client.Connect(ctx, wsURL, runtimeFixtureToken, runtimeFixtureBuildID)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	startPayload, _ := json.Marshal(contract.SessionStartOneShotPayload{
		DisplayName:          "Test Session",
		InstitutionProfileID: "jlu",
		Username:             "fictional-user-9Z1",
		Password:             "fictional-password-9Z2",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: "automatically_select_latest_available"},
	})
	response, err := conn.Call(ctx, contract.MethodSessionStartOneShot, startPayload)
	if err != nil {
		t.Fatalf("session.startOneShot call: %v", err)
	}
	if !response.OK {
		t.Fatalf("session.startOneShot error: %+v", response.Error)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "event=session_snapshot") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(buf.String(), "event=session_snapshot") {
		t.Fatalf("expected session_snapshot event in log, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "fictional-password-9Z2") {
		t.Fatalf("password leaked into diagnostics log:\n%s", buf.String())
	}
}

func TestCompositionConnectsSafePostCommitDiagnosticObserver(t *testing.T) {
	var logs bytes.Buffer
	rt, err := composeObjectGraph(context.Background(), newInMemoryStore(), testPaths(t), testHostInfo(), newFakeObserver(), newFakeHostRunner().run, "token", "v", "b", bufferLogger(&logs))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.shutdown.Close(); rt.shutdown.Wait() }()
	if rt.hostCfg.OnPostCommitDiagnostic == nil {
		t.Fatal("production Host observer missing")
	}
	rt.hostCfg.OnPostCommitDiagnostic(errors.New("password-marker /private/path cause-marker"))
	out := logs.String()
	if strings.Count(out, "event="+eventStoragePostCommitDiagnostic) != 1 {
		t.Fatal("production warning missing or repeated")
	}
	for _, secret := range []string{"password-marker", "/private/path", "cause-marker", "error="} {
		if strings.Contains(out, secret) {
			t.Fatal("production Host warning leaked cause")
		}
	}
}
