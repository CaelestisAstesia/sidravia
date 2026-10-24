package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

const networkTraceInterval = 30 * time.Second
const networkTraceTimeout = 5 * time.Second
const networkTraceOutputLimit = 64 * 1024

type networkTraceRequest struct {
	SessionID   string
	Failure     protocol.AuthenticationProtocolNetworkFailure
	TriggeredAt time.Time
}

type networkTraceSection struct {
	Status     string          `json:"status"`
	SystemCode int64           `json:"systemCode"`
	Limit      int             `json:"limit,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type networkTraceFacts struct {
	UDP           networkTraceSection `json:"udp"`
	ExcludedPorts networkTraceSection `json:"excludedPorts"`
	Addresses     networkTraceSection `json:"addresses"`
	Route         networkTraceSection `json:"route"`
	Events        networkTraceSection `json:"events"`
}

type networkTraceResult struct {
	Status string
	Facts  networkTraceFacts
}

// One runtime-owned worker performs diagnostics outside the protocol goroutine.
// Requests are bounded and globally rate limited; shutdown cancels and joins it.
type networkTraceRecorder struct {
	logger       *slog.Logger
	collect      func(context.Context, networkTraceRequest) networkTraceResult
	requests     chan networkTraceRequest
	mu           sync.Mutex
	lastAccepted time.Time
	stopped      bool
}

func newNetworkTraceRecorder(logger *slog.Logger) *networkTraceRecorder {
	if !logger.Enabled(context.Background(), LevelTrace) {
		return nil
	}
	return &networkTraceRecorder{logger: logger, collect: collectNetworkTrace, requests: make(chan networkTraceRequest, 1)}
}

func (r *networkTraceRecorder) submit(request networkTraceRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		r.logger.Log(context.Background(), LevelTrace, "网络现场采集已跳过", slog.String("event", "network_failure_snapshot_skipped"), slog.String("session_id", request.SessionID), slog.String("status", "stopped"))
		return
	}
	status := "rate_limited"
	if r.lastAccepted.IsZero() || request.TriggeredAt.Sub(r.lastAccepted) >= networkTraceInterval {
		select {
		case r.requests <- request:
			r.lastAccepted = request.TriggeredAt
			return
		default:
			status = "queue_full"
		}
	}
	r.logger.Log(context.Background(), LevelTrace, "网络现场采集已跳过",
		slog.String("event", "network_failure_snapshot_skipped"), slog.String("session_id", request.SessionID), slog.String("status", status))
}

func (r *networkTraceRecorder) run(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stopped = true
		select {
		case request := <-r.requests:
			r.logger.Log(context.Background(), LevelTrace, "网络现场采集已跳过", slog.String("event", "network_failure_snapshot_skipped"), slog.String("session_id", request.SessionID), slog.String("status", "stopped"))
		default:
		}
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case request := <-r.requests:
			if ctx.Err() != nil {
				return
			}
			observedAt := time.Now()
			sampleCtx, cancel := context.WithTimeout(ctx, networkTraceTimeout)
			result := r.collect(sampleCtx, request)
			cancel()
			facts, err := json.Marshal(result.Facts)
			if err != nil {
				result.Status = "invalid_result"
				facts = []byte("{}")
			}
			r.logger.Log(context.Background(), LevelTrace, "网络失败现场快照",
				slog.String("event", "network_failure_snapshot"), slog.String("session_id", request.SessionID),
				slog.String("operation", request.Failure.Operation), slog.String("system_call", request.Failure.SystemCall),
				slog.Uint64("system_code", request.Failure.SystemCode),
				slog.Bool("timed_out", request.Failure.TimedOut),
				slog.String("local_endpoint", request.Failure.LocalEndpoint.String()), slog.String("remote_endpoint", request.Failure.RemoteEndpoint.String()),
				slog.Time("triggered_at", request.TriggeredAt), slog.Time("collection_started_at", observedAt), slog.Time("collection_finished_at", time.Now()),
				slog.String("status", result.Status), slog.String("facts_json", string(facts)))
		}
	}
}

// boundedTraceOutput drains process pipes without retaining unbounded output.
// Overflow invalidates the sample; no partial JSON or raw stderr is logged.
type boundedTraceOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedTraceOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	b.data = append(b.data, p...)
	return n, nil
}
