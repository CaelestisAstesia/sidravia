package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/connection/auth/d520/internal/settings"
	"sidravia/internal/connection/auth/protocol"
	profilevalue "sidravia/internal/connection/profile"
)

// Exercise real sockets through authentication, network loss or manual stop,
// cleanup and immediate authentication on the same source port. All traffic is
// loopback; no host interface, hotspot or system policy is changed.
func TestFixedPortReopensAfterHeartbeatLossAndManualStop(t *testing.T) {
	for _, stop := range []string{"heartbeat_loss", "manual_stop"} {
		t.Run(stop, func(t *testing.T) {
			peer := newTestPeer(t, defaultPeerResponder())
			port := uint16(freeUDPPort(t, net.IPv4(127, 0, 0, 1)))
			durations := defaultTestDurations()
			durations.keepalive = 50 * time.Millisecond
			durations.logout = 50 * time.Millisecond
			newRun := func() *d520Run {
				run := buildTestRun(t, peer, durations, testCredential()).(*d520Run)
				run.definition.cfg.LocalPort = settings.LocalPort{Mode: settings.LocalPortFixed, Value: port}
				return run
			}
			first := &socketRecordingObserver{recordingObserver: newRecordingObserver(peer)}
			_, cancel, done := startRun(t, newRun(), first)
			defer cancel(context.Canceled)
			first.waitForEstablished(t, time.Second)
			if stop == "heartbeat_loss" {
				peer.setRespond(nil) // Drop heartbeat and bounded logout, like the log.
				failure := assertRunReturns(t, done, false, 2*time.Second)
				if failure.Code != "network_timeout" {
					t.Fatalf("network loss = %q, want network_timeout", failure.Code)
				}
			} else {
				cancel(protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: protocol.TerminateWithBestEffortLogout})
				assertRunReturns(t, done, true, time.Second)
			}
			if len(first.closed) != 1 || !first.closed[0] {
				t.Fatalf("old socket not confirmed closed: %v", first.closed)
			}
			peer.setRespond(defaultPeerResponder())
			second := &socketRecordingObserver{recordingObserver: newRecordingObserver(peer)}
			_, cancelSecond, secondDone := startRun(t, newRun(), second)
			defer cancelSecond(context.Canceled)
			second.waitForEstablished(t, time.Second)
			cancelSecond(context.Canceled)
			assertRunReturns(t, secondDone, true, time.Second)
			if len(first.opened) != 1 || len(second.opened) != 1 || first.opened[0].local != second.opened[0].local {
				t.Fatalf("source endpoint changed: first=%v second=%v", first.opened, second.opened)
			}
			assertSocketPortReleased(t, second.opened[0].local)
		})
	}
}

func TestRunCancellationWakesReadAndPreservesSocket(t *testing.T) {
	d := defaultTestDurations()
	d.keepalive = 1 * time.Second
	var ka2Count int
	var bootstrapDone bool
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess([16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10})}
		case isKA1Req(req):
			if !bootstrapDone {
				return [][]byte{peerKA1Response()}
			}
			return nil // drop heartbeat KA1 -> Run blocks on in-flight read
		case isKA2Req(req):
			if !bootstrapDone {
				ka2Count++
				if ka2Count == 3 {
					bootstrapDone = true
				}
				return [][]byte{peerKA2Response(req[1], req[5], [4]byte{req[1], req[5], 0xaa, 0xbb})}
			}
			return nil
		case isLogoutReq(req):
			return [][]byte{peerLogoutACK()}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, d, testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	observer.waitForEstablished(t, time.Second)
	// Wait for the heartbeat KA1 to be received and dropped so the Run is
	// blocked on the read.
	waitForRequestCount(t, peer, 7, time.Second)

	start := time.Now()
	cancel(protocol.AuthenticationProtocolRunCancellationCause{
		CleanupRequirement: protocol.TerminateWithBestEffortLogout,
		Description:        "test cancellation",
	})
	failure := assertRunReturns(t, done, true, 2*time.Second)
	elapsed := time.Since(start)
	if elapsed >= d.keepalive {
		t.Fatalf("cancellation took %s, want < keepalive timeout %s (read was not woken promptly)", elapsed, d.keepalive)
	}
	if failure != nil {
		t.Fatalf("expected nil failure on cancellation, got %q", failure.Code)
	}
	// The socket was preserved: fresh-salt Logout Challenge and Logout were
	// sent on the same socket after cancellation.
	waitForOp(t, peer, "logout", time.Second)
	ops := peer.requestOpNames()
	if countOps(ops, "challenge") < 2 {
		t.Fatalf("expected fresh-salt logout challenge after cancellation: ops=%v", ops)
	}
	if countOps(ops, "logout") != 1 {
		t.Fatalf("expected one logout after cancellation: ops=%v", ops)
	}
}

// 7. failed fresh Logout Challenge sends one fallback Logout using login salt.
type observedSocketEndpoints struct{ local, remote netip.AddrPort }
type socketRecordingObserver struct {
	*recordingObserver
	opened           []observedSocketEndpoints
	closed           []bool
	openingError     error
	openedAtRequests int
}

func (o *socketRecordingObserver) ProtocolSocketOpened(local, remote netip.AddrPort) error {
	o.opened = append(o.opened, observedSocketEndpoints{local, remote})
	if o.peer != nil {
		o.openedAtRequests = len(o.peer.requests())
	}
	return o.openingError
}
func (o *socketRecordingObserver) ProtocolSocketClosed(closed bool) {
	o.closed = append(o.closed, closed)
}

func assertSocketPortReleased(t *testing.T, endpoint netip.AddrPort) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(endpoint))
	if err != nil {
		t.Fatalf("owned socket port still occupied after Execute: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRunSocketClosesAfterNegativeAuthentication(t *testing.T) {
	respond := defaultPeerResponder()
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if isLoginReq(req) {
			return [][]byte{peerLoginRejection(0x03)}
		}
		return respond(req)
	})
	observer := &socketRecordingObserver{recordingObserver: newRecordingObserver(peer)}
	failure := buildTestRun(t, peer, defaultTestDurations(), testCredential()).Execute(context.Background(), observer)
	if failure == nil || failure.Code != "credential_invalid" {
		t.Fatalf("failure = %#v", failure)
	}
	if len(observer.opened) != 1 || len(observer.closed) != 1 || !observer.closed[0] {
		t.Fatalf("observations = %#v / %v", observer.opened, observer.closed)
	}
	if observer.openedAtRequests != 0 {
		t.Fatal("opening callback followed first exchange")
	}
	assertSocketPortReleased(t, observer.opened[0].local)
}

func TestRunSocketOpenFailureHasNoObservations(t *testing.T) {
	peer := newTestPeer(t, defaultPeerResponder())
	occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	inputs := factoryInputs(testConfigJSON(peer.port(), defaultTestDurations()), testCredential(), testBinding(t))
	inputs.InstitutionProtocolConfiguration = profilevalue.ProtocolConfiguration(bytes.Replace(inputs.InstitutionProtocolConfiguration, []byte(`{"mode": "system_assigned"}`), []byte(fmt.Sprintf(`{"mode":"fixed","value":%d}`, occupied.LocalAddr().(*net.UDPAddr).Port)), 1))
	diagnostics := &captureDiagnostics{}
	inputs.Diagnostics = diagnostics
	run, err := NewFactory().CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatal(err)
	}
	observer := &socketRecordingObserver{recordingObserver: newRecordingObserver(peer)}
	failure := run.Execute(context.Background(), observer)
	if failure == nil || failure.Code != "network_io_failed" {
		t.Fatalf("failure = %#v", failure)
	}
	if len(diagnostics.networkFailures) != 1 || diagnostics.networkFailures[0].Operation != "open_socket" || diagnostics.networkFailures[0].SystemCode == 0 {
		t.Fatalf("missing native socket error: %+v", diagnostics.networkFailures)
	}
	if len(observer.opened) != 0 || len(observer.closed) != 0 || len(peer.requests()) != 0 {
		t.Fatal("failed open invented an observation or exchanged data")
	}
}

func TestRunRejectedOpeningPreservesCauseAndClosesWithoutCallback(t *testing.T) {
	peer := newTestPeer(t, defaultPeerResponder())
	cause := errors.New("observer rejected socket contract")
	observer := &socketRecordingObserver{recordingObserver: newRecordingObserver(peer), openingError: cause}
	failure := buildTestRun(t, peer, defaultTestDurations(), testCredential()).Execute(context.Background(), observer)
	if failure == nil || failure.Code != "protocol_contract_violated" || !errors.Is(failure.DiagnosticCause, cause) {
		t.Fatalf("failure = %#v", failure)
	}
	if len(observer.opened) != 1 || len(observer.closed) != 0 || len(peer.requests()) != 0 {
		t.Fatal("rejected opening exchanged data or emitted closure")
	}
	assertSocketPortReleased(t, observer.opened[0].local)
}
