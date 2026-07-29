package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
)

// LevelTrace is the custom slog level below Debug. It enables complete D520
// datagram logging, which may contain account and authentication material, so
// it is explicitly sensitive and never enabled by default.
var LevelTrace = slog.Level(-8)

// Exact lowercase values accepted by SIDRAVIA_LOG_LEVEL. The empty value means
// Info. Invalid values cause safe daemon construction failure without echoing
// the supplied value.
const (
	logLevelInfo  = "info"
	logLevelDebug = "debug"
	logLevelTrace = "trace"
)

// Operational event codes and fixed Simplified Chinese messages for tiered
// diagnostics. Messages are constant summaries; they are never constructed
// from an error, snapshot, request or response. Only the attributes listed in
// the stable schema ever appear on a record.
const (
	eventSessionSnapshot              = "session_snapshot"
	eventSessionCommand               = "session_command"
	eventProtocolRunGeneration        = "protocol_run_generation"
	eventRetryScheduled               = "retry_scheduled"
	eventProtocolPhase                = "protocol_phase"
	eventProtocolDatagram             = "protocol_datagram"
	eventTraceLoggingSensitive        = "trace_logging_sensitive"
	eventStorageProtectionUnavailable = "storage_protection_unavailable"

	msgSessionSnapshot              = "Session 状态已更新"
	msgSessionCommand               = "Session 命令已接收"
	msgProtocolRunGeneration        = "认证协议运行已创建"
	msgRetryScheduled               = "认证重试已调度"
	msgProtocolPhase                = "协议阶段边界"
	msgProtocolDatagram             = "协议数据报"
	msgTraceLoggingSensitive        = "Trace 日志含敏感信息"
	msgStorageProtectionUnavailable = "便携存储无法使用当前用户权限保护"
)

// resolveLogLevel maps SIDRAVIA_LOG_LEVEL to a slog level. The empty value
// means Info. An invalid value returns an error without echoing the value.
func resolveLogLevel(value string) (slog.Level, error) {
	switch value {
	case "", logLevelInfo:
		return slog.LevelInfo, nil
	case logLevelDebug:
		return slog.LevelDebug, nil
	case logLevelTrace:
		return LevelTrace, nil
	default:
		return 0, fmt.Errorf("sidraviad: unsupported log level")
	}
}

// traceLoggingEnabled reports whether the level enables Trace records.
func traceLoggingEnabled(level slog.Level) bool {
	return level <= LevelTrace
}

// sessionDiagnostics is the production session.Diagnostics adapter. It writes
// fixed Simplified Chinese messages and only the allowed attributes for each
// event. It never logs password, Credential ID, Interface ID, MAC, DNS/DHCP,
// token, request/response bytes, raw errors or diagnostic causes. The sink has
// no return value and cannot change Session decisions.
type sessionDiagnostics struct {
	logger *slog.Logger
}

func (d *sessionDiagnostics) SessionSnapshot(snapshot session.Snapshot) {
	attrs := []any{
		slog.String("event", eventSessionSnapshot),
		slog.String("session_id", string(snapshot.AuthenticationSessionID)),
		slog.Uint64("revision", snapshot.Revision),
		slog.String("state", string(snapshot.State)),
		slog.String("intent", string(snapshot.Intent)),
		slog.String("institution_profile_id", string(snapshot.InstitutionProfileID)),
		slog.String("account_name", snapshot.AccountName),
		slog.String("authentication_protocol_id", string(snapshot.AuthenticationProtocolID)),
	}
	if snapshot.InstitutionDisplayName != "" {
		attrs = append(attrs, slog.String("institution_display_name", snapshot.InstitutionDisplayName))
	}
	if snapshot.SelectedNetworkBinding != nil {
		attrs = append(attrs, slog.String("selected_interface_name", snapshot.SelectedNetworkBinding.DisplayName))
		attrs = append(attrs, slog.String("selected_ipv4", snapshot.SelectedNetworkBinding.LocalIPv4Address.String()))
	}
	if snapshot.StateReason != nil {
		attrs = append(attrs, slog.String("state_reason_code", snapshot.StateReason.Code))
	}
	if snapshot.AuthenticationEstablishedAt != nil {
		attrs = append(attrs, slog.String("authentication_established_at", snapshot.AuthenticationEstablishedAt.Format(time.RFC3339Nano)))
	}
	if snapshot.NextRetryAt != nil {
		attrs = append(attrs, slog.String("next_retry_at", snapshot.NextRetryAt.Format(time.RFC3339Nano)))
	}
	if snapshot.LastAuthenticationFailure != nil {
		attrs = append(attrs, slog.String("failure_code", string(snapshot.LastAuthenticationFailure.Code)))
		attrs = append(attrs, slog.String("recommendation_code", string(snapshot.LastAuthenticationFailure.HandlingRecommendation)))
	}
	d.logger.Info(msgSessionSnapshot, attrs...)
}

func (d *sessionDiagnostics) SessionCommand(command string) {
	d.logger.Debug(msgSessionCommand,
		slog.String("event", eventSessionCommand),
		slog.String("command", command),
	)
}

func (d *sessionDiagnostics) ProtocolRunGeneration(generation uint64) {
	d.logger.Debug(msgProtocolRunGeneration,
		slog.String("event", eventProtocolRunGeneration),
		slog.Uint64("generation", generation),
	)
}

func (d *sessionDiagnostics) RetryScheduled(nextRetryAt time.Time) {
	d.logger.Debug(msgRetryScheduled,
		slog.String("event", eventRetryScheduled),
		slog.String("next_retry_at", nextRetryAt.Format(time.RFC3339Nano)),
	)
}

// protocolDiagnosticsAdapter is the production
// protocol.AuthenticationProtocolDiagnostics adapter bound to one Session. It
// writes phase boundaries at Debug and the complete lowercase datagram hex at
// Trace. The SessionID is carried here so the protocol run never knows it; the
// run passes only a stable phase and raw bytes, never fields derived from
// packet contents.
type protocolDiagnosticsAdapter struct {
	logger    *slog.Logger
	sessionID string
}

func (d *protocolDiagnosticsAdapter) PhaseEvent(phase, boundary string) {
	d.logger.Debug(msgProtocolPhase,
		slog.String("event", eventProtocolPhase),
		slog.String("session_id", d.sessionID),
		slog.String("phase", phase),
		slog.String("boundary", boundary),
	)
}

func (d *protocolDiagnosticsAdapter) DatagramEvent(phase string, direction protocol.AuthenticationProtocolDatagramDirection, datagram []byte) {
	if !d.logger.Enabled(context.Background(), LevelTrace) {
		return
	}
	d.logger.Log(context.Background(), LevelTrace, msgProtocolDatagram,
		slog.String("event", eventProtocolDatagram),
		slog.String("session_id", d.sessionID),
		slog.String("phase", phase),
		slog.String("direction", string(direction)),
		slog.Int("length", len(datagram)),
		slog.String("datagram_hex", hex.EncodeToString(datagram)),
	)
}

// newSessionDiagnostics returns the production session.Diagnostics adapter.
func newSessionDiagnostics(logger *slog.Logger) session.Diagnostics {
	return &sessionDiagnostics{logger: logger}
}

// newProtocolDiagnosticsFactory returns a session.ProtocolDiagnosticsFactory
// that binds each protocol diagnostics sink to the Session that owns the
// protocol run, so Trace datagram records include the SessionID without the
// protocol run ever knowing it.
func newProtocolDiagnosticsFactory(logger *slog.Logger) session.ProtocolDiagnosticsFactory {
	return func(sessionID session.AuthenticationSessionID) protocol.AuthenticationProtocolDiagnostics {
		return &protocolDiagnosticsAdapter{logger: logger, sessionID: string(sessionID)}
	}
}

func newStorageProtectionWarning(logger *slog.Logger) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			logger.Warn(msgStorageProtectionUnavailable, slog.String("event", eventStorageProtectionUnavailable))
		})
	}
}
