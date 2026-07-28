package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"net/netip"
	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
	environment "sidravia/internal/daemon/environment"
)

func TestResolveLogLevelAcceptsExactValues(t *testing.T) {
	cases := []struct {
		value string
		want  slog.Level
	}{
		{"", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"trace", LevelTrace},
	}
	for _, c := range cases {
		got, err := resolveLogLevel(c.value)
		if err != nil {
			t.Errorf("resolveLogLevel(%q) = %v, want nil", c.value, err)
		}
		if got != c.want {
			t.Errorf("resolveLogLevel(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestResolveLogLevelRejectsInvalidWithoutEchoingValue(t *testing.T) {
	const marker = "SUPER-SECRET-LEVEL-MARKER-9F3A"
	_, err := resolveLogLevel(marker)
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("invalid level value echoed in error: %v", err)
	}
	// Case-sensitive: uppercase variants are invalid.
	for _, invalid := range []string{"INFO", "Debug", "TRACE"} {
		if _, err := resolveLogLevel(invalid); err == nil {
			t.Errorf("resolveLogLevel(%q) = nil, want error", invalid)
		}
	}
}

func TestTraceLoggingEnabled(t *testing.T) {
	if traceLoggingEnabled(slog.LevelInfo) {
		t.Error("Info must not enable Trace")
	}
	if traceLoggingEnabled(slog.LevelDebug) {
		t.Error("Debug must not enable Trace")
	}
	if !traceLoggingEnabled(LevelTrace) {
		t.Error("Trace must enable Trace")
	}
}

func TestNewDaemonLoggerHonorsLevel(t *testing.T) {
	t.Run("info drops debug", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newDaemonLogger(&buf, slog.LevelInfo)
		logger.Debug("debug marker", slog.String("event", "debug_event"))
		if strings.Contains(buf.String(), "debug marker") {
			t.Fatalf("Debug record was not dropped at Info level, got:\n%s", buf.String())
		}
	})
	t.Run("debug keeps debug", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newDaemonLogger(&buf, slog.LevelDebug)
		logger.Debug("debug marker", slog.String("event", "debug_event"))
		if !strings.Contains(buf.String(), "debug marker") {
			t.Fatalf("Debug record was dropped at Debug level, got:\n%s", buf.String())
		}
	})
}

func diagnosticTestSnapshot() session.Snapshot {
	established := time.Unix(100, 0)
	retry := time.Unix(200, 0)
	addr := netip.MustParseAddr("192.0.2.10")
	return session.Snapshot{
		AuthenticationSessionID:  "session-1",
		InstitutionProfileID:     config.InstitutionProfileID("jlu"),
		InstitutionDisplayName:   "吉林大学",
		AuthenticationProtocolID: protocol.AuthenticationProtocolID("drcom-5.2.0-d"),
		AccountName:              "alice2024",
		Intent:                   session.MaintainAuthentication,
		State:                    session.Authenticated,
		Revision:                 7,
		SelectedNetworkBinding: &session.NetworkBindingSummary{
			InterfaceID:      environment.InterfaceID("if-7"),
			DisplayName:      "Ethernet",
			LocalIPv4Address: addr,
		},
		AuthenticationEstablishedAt: &established,
		NextRetryAt:                 &retry,
		LastAuthenticationFailure: &session.AuthenticationFailure{
			Code:                   protocol.AuthenticationProtocolFailureCode("network_timeout"),
			HandlingRecommendation: protocol.RetryAfterStandardDelay,
		},
	}
}

func TestSessionDiagnosticsSnapshotWritesAllowedFieldsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	d := newSessionDiagnostics(logger)
	d.SessionSnapshot(diagnosticTestSnapshot())
	output := buf.String()
	// Allowed fields appear.
	for _, want := range []string{
		"event=session_snapshot",
		"session_id=session-1",
		"revision=7",
		"state=authenticated",
		"institution_profile_id=jlu",
		"account_name=alice2024",
		"selected_interface_name=Ethernet",
		"selected_ipv4=192.0.2.10",
		"failure_code=network_timeout",
		"recommendation_code=retry_after_standard_delay",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in:\n%s", want, output)
		}
	}
	// Forbidden: machine Interface ID never appears in human output.
	if strings.Contains(output, "if-7") {
		t.Errorf("InterfaceID leaked into session diagnostics:\n%s", output)
	}
	if strings.Contains(output, "interface_id=") {
		t.Errorf("interface_id attribute leaked:\n%s", output)
	}
}

func TestSessionDiagnosticsDebugEventsDroppedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	d := newSessionDiagnostics(logger)
	d.SessionCommand("activate")
	d.ProtocolRunGeneration(1)
	d.RetryScheduled(time.Unix(300, 0))
	if strings.Contains(buf.String(), "event=session_command") ||
		strings.Contains(buf.String(), "event=protocol_run_generation") ||
		strings.Contains(buf.String(), "event=retry_scheduled") {
		t.Fatalf("Debug events must be dropped at Info level, got:\n%s", buf.String())
	}
}

func TestSessionDiagnosticsDebugEventsKeptAtDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d := newSessionDiagnostics(logger)
	d.SessionCommand("activate")
	d.ProtocolRunGeneration(1)
	d.RetryScheduled(time.Unix(300, 0))
	output := buf.String()
	for _, want := range []string{
		"event=session_command",
		"command=activate",
		"event=protocol_run_generation",
		"generation=1",
		"event=retry_scheduled",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in:\n%s", want, output)
		}
	}
}

func TestProtocolDiagnosticsDatagramHexOnlyAtTrace(t *testing.T) {
	t.Run("trace writes complete lowercase hex", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelTrace}))
		d := &protocolDiagnosticsAdapter{logger: logger, sessionID: "session-1"}
		datagram := []byte{0x01, 0x02, 0xab, 0xcd}
		d.DatagramEvent("challenge", protocol.DatagramDirectionTx, datagram)
		output := buf.String()
		for _, want := range []string{
			"event=protocol_datagram",
			"session_id=session-1",
			"phase=challenge",
			"direction=tx",
			"length=4",
			"datagram_hex=0102abcd",
		} {
			if !strings.Contains(output, want) {
				t.Errorf("missing %q in:\n%s", want, output)
			}
		}
	})
	t.Run("info drops datagram record", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
		d := &protocolDiagnosticsAdapter{logger: logger, sessionID: "session-1"}
		d.DatagramEvent("challenge", protocol.DatagramDirectionTx, []byte{0x01, 0x02})
		if strings.Contains(buf.String(), "event=protocol_datagram") {
			t.Fatalf("Info level must drop Trace datagram, got:\n%s", buf.String())
		}
		if strings.Contains(buf.String(), "0102") {
			t.Fatalf("datagram hex leaked at Info level:\n%s", buf.String())
		}
	})
	t.Run("debug drops datagram record", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		d := &protocolDiagnosticsAdapter{logger: logger, sessionID: "session-1"}
		d.DatagramEvent("challenge", protocol.DatagramDirectionRx, []byte{0x01, 0x02})
		if strings.Contains(buf.String(), "event=protocol_datagram") {
			t.Fatalf("Debug level must drop Trace datagram, got:\n%s", buf.String())
		}
	})
}

func TestProtocolDiagnosticsPhaseEventAtDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d := &protocolDiagnosticsAdapter{logger: logger, sessionID: "session-1"}
	d.PhaseEvent("login", "begin")
	output := buf.String()
	for _, want := range []string{
		"event=protocol_phase",
		"session_id=session-1",
		"phase=login",
		"boundary=begin",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in:\n%s", want, output)
		}
	}
}

func TestProtocolDiagnosticsFactoryBindsSessionID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: LevelTrace}))
	factory := newProtocolDiagnosticsFactory(logger)
	sink := factory(session.AuthenticationSessionID("session-42"))
	sink.DatagramEvent("logout", protocol.DatagramDirectionTx, []byte{0xff})
	// The adapter type carries the session id; verify via a fresh buffer.
	var buf bytes.Buffer
	logger2 := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelTrace}))
	factory2 := newProtocolDiagnosticsFactory(logger2)
	factory2(session.AuthenticationSessionID("session-42")).DatagramEvent("logout", protocol.DatagramDirectionTx, []byte{0xff})
	if !strings.Contains(buf.String(), "session_id=session-42") {
		t.Fatalf("session id not bound by factory:\n%s", buf.String())
	}
}
