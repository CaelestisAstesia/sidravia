package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNetworkTraceDisabledOutsideTrace(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		if r := newNetworkTraceRecorder(newDaemonLogger(&bytes.Buffer{}, level)); r != nil {
			t.Fatal("collector allocated outside Trace")
		}
	}
}

func TestNetworkTraceBoundedQueueAndRateLimit(t *testing.T) {
	r := newNetworkTraceRecorder(newDaemonLogger(&bytes.Buffer{}, LevelTrace))
	start := time.Now()
	r.submit(networkTraceRequest{TriggeredAt: start})
	r.submit(networkTraceRequest{TriggeredAt: start.Add(time.Second)})
	r.submit(networkTraceRequest{TriggeredAt: start.Add(networkTraceInterval)}) // queue still full
	if len(r.requests) != 1 || !r.lastAccepted.Equal(start) {
		t.Fatal("queue or accepted timestamp changed on dropped sample")
	}
	<-r.requests
	r.submit(networkTraceRequest{TriggeredAt: start.Add(networkTraceInterval)})
	if len(r.requests) != 1 {
		t.Fatal("next interval sample lost")
	}
}

func TestNetworkTraceWorkerCancellationIsWaitable(t *testing.T) {
	var buf bytes.Buffer
	r := newNetworkTraceRecorder(newDaemonLogger(&buf, LevelTrace))
	entered := make(chan struct{})
	r.collect = func(ctx context.Context, request networkTraceRequest) networkTraceResult {
		close(entered)
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > networkTraceTimeout {
			t.Error("missing bounded collection deadline")
		}
		<-ctx.Done()
		return networkTraceResult{Status: "cancelled_or_timed_out"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.run(ctx) }()
	r.submit(networkTraceRequest{SessionID: "session-test", TriggeredAt: time.Now()})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("collector not started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector not joined")
	}
	if !strings.Contains(buf.String(), "status=cancelled_or_timed_out") {
		t.Fatalf("missing completion status: %s", buf.String())
	}
}

func TestNetworkTraceBoundedOutputDiscardsOverflow(t *testing.T) {
	out := boundedTraceOutput{limit: 8}
	for _, part := range []string{"12345", "67890", "further output"} {
		n, err := out.Write([]byte(part))
		if n != len(part) || err != nil {
			t.Fatal("pipe was not drained")
		}
	}
	if len(out.data) != 8 || !out.overflow {
		t.Fatalf("output=%+v", out)
	}
}

func TestNetworkTraceSnapshotIncludesCollectionTimes(t *testing.T) {
	var buf bytes.Buffer
	r := newNetworkTraceRecorder(newDaemonLogger(&buf, LevelTrace))
	ctx, cancel := context.WithCancel(context.Background())
	r.collect = func(context.Context, networkTraceRequest) networkTraceResult {
		cancel()
		return networkTraceResult{Status: "collected", Facts: networkTraceFacts{UDP: networkTraceSection{Status: "collected", Data: json.RawMessage(`[]`)}}}
	}
	r.submit(networkTraceRequest{SessionID: "session-test", TriggeredAt: time.Now()})
	r.run(ctx)
	for _, key := range []string{"event=network_failure_snapshot", "triggered_at=", "collection_started_at=", "collection_finished_at=", "facts_json="} {
		if !strings.Contains(buf.String(), key) {
			t.Fatalf("missing %s: %s", key, buf.String())
		}
	}
}

func TestNetworkTraceRejectsSamplesAfterWorkerStops(t *testing.T) {
	var buf bytes.Buffer
	r := newNetworkTraceRecorder(newDaemonLogger(&buf, LevelTrace))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.run(ctx)
	r.submit(networkTraceRequest{TriggeredAt: time.Now()})
	if len(r.requests) != 0 || !strings.Contains(buf.String(), "status=stopped") {
		t.Fatal("accepted sample without a running worker")
	}
}
