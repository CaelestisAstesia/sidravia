package d520

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

func TestExperimentalLateRepliesKeepOnlineAndFinishOriginalStages(t *testing.T) {
	base := experimentalResponder()
	var delay atomic.Bool
	var delayed atomic.Int32
	peer := newTestPeer(t, func(request []byte) [][]byte {
		response := base(request)
		if delay.Load() && len(response) > 0 {
			delayed.Add(1)
			time.Sleep(100 * time.Millisecond)
		}
		return response
	})
	durations := defaultTestDurations()
	// Keep the same late-reply ratios without relying on 10ms host scheduling.
	durations.keepalive = 50 * time.Millisecond
	durations.heartbeat = 150 * time.Millisecond
	run := experimentalTestRun(t, peer, durations)
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, 3*time.Second)
	delay.Store(true)
	// Exceeds twice the 750ms online window. All subsequent replies arrive
	// after their 50ms request deadlines, while the sole reader is recovering.
	select {
	case failure := <-done:
		t.Fatalf("late valid replies lost: %#v", failure)
	case <-time.After(1750 * time.Millisecond):
	}
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	if delayed.Load() < 6 {
		t.Fatalf("too few delayed exchanges: %d", delayed.Load())
	}
	var types []byte
	for _, request := range peer.requests() {
		if len(request) == 40 && request[0] == 7 {
			types = append(types, request[5])
		}
	}
	if len(types) < 6 {
		t.Fatal("late module responses never completed cycles")
	}
	for i, typ := range types {
		want := byte(1)
		if i%2 == 1 {
			want = 3
		}
		if typ != want {
			t.Fatalf("module stage %d = %d", i, typ)
		}
	}
	if observer.establishedCallCount() != 1 {
		t.Fatal("late replies established another authentication")
	}
}

func replyTestExecution() *execution {
	exec := newExecution(runDefinition{}, nil, protocol.NoopAuthenticationProtocolDiagnostics{})
	exec.hasAuthInfo = true
	exec.experimental.interval = time.Second
	exec.experimental.lastEvidence = time.Now().Add(-time.Second)
	return exec
}

func TestExperimentalLateReplyLifetimeDuplicatesAndSupersession(t *testing.T) {
	ping := []byte{7, 0, 6, 0, 6, 0}
	exec := replyTestExecution()
	initial := exec.experimental.lastEvidence
	if exec.receiveLateReply(ping) || exec.experimental.lastEvidence != initial {
		t.Fatal("unsolicited ping kept online")
	}
	exec.experimental.ping = &pendingPing{late: true, expires: time.Now().Add(time.Second)}
	if !exec.receiveLateReply(ping) {
		t.Fatal("pending ping not recognized")
	}
	evidence := exec.experimental.lastEvidence
	if exec.receiveLateReply(ping) || exec.experimental.lastEvidence != evidence {
		t.Fatal("duplicate ping refreshed evidence")
	}
	if !exec.consumePingReply() || exec.consumePingReply() {
		t.Fatal("ping result consumed more than once")
	}
	exec.experimental.ping = &pendingPing{late: true, expires: time.Now().Add(-time.Second)}
	if exec.receiveLateReply(ping) || exec.experimental.lastEvidence != evidence {
		t.Fatal("expired ping kept online")
	}
	exec.experimental.ping = &pendingPing{expires: time.Now().Add(time.Second)}
	if exec.receiveLateReply(ping) {
		t.Fatal("active send handled as a late response")
	}
	for _, unknown := range [][]byte{{7}, {7, 0, 6, 0, 99, 0}, {7, 0, 6, 0, 4, 0}, {0x4d, 0x38, 0, 0}} {
		if exec.receiveLateReply(unknown) || exec.experimental.lastEvidence != evidence {
			t.Fatal("unrelated traffic renewed evidence")
		}
	}
}

func TestExperimentalLateModuleCorrelationAndEvidence(t *testing.T) {
	for _, typ := range []byte{1, 3} {
		exec := replyTestExecution()
		initial := exec.experimental.lastEvidence
		flight := &pendingModule{serial: 9, typ: typ, nonce: [4]byte{1, 2, 3, 4}, late: true, expires: time.Now().Add(time.Second)}
		exec.experimental.module = flight
		packet := buildExperimentalModule(9, typ+1, 732, flight.nonce, [8]byte{8, 7, 6, 5, 4, 3, 2, 1}, [4]byte{})
		wrong := append([]byte(nil), packet...)
		wrong[8]++
		if exec.receiveLateReply(wrong) {
			t.Fatal("wrong nonce accepted")
		}
		wrong = append([]byte(nil), packet...)
		wrong[1]++
		if exec.receiveLateReply(wrong) {
			t.Fatal("wrong serial accepted")
		}
		if !exec.receiveLateReply(packet) || flight.reply == nil {
			t.Fatal("late module lost")
		}
		if typ == 1 && exec.experimental.lastEvidence != initial {
			t.Fatal("Type2 became online evidence")
		}
		if typ == 3 && exec.experimental.lastEvidence == initial {
			t.Fatal("Type4 not online evidence")
		}
		evidence := exec.experimental.lastEvidence
		if exec.receiveLateReply(packet) || exec.experimental.lastEvidence != evidence {
			t.Fatal("duplicate module renewed evidence")
		}
		exec.experimental.module = &pendingModule{serial: 9, typ: typ, nonce: flight.nonce, late: true, expires: time.Now().Add(-time.Second)}
		if exec.receiveLateReply(packet) {
			t.Fatal("expired module accepted")
		}
		// Reusing the serial does not rescue a reply from a replaced request.
		exec.experimental.module = &pendingModule{serial: 9, typ: typ, nonce: [4]byte{2, 2, 3, 4}, late: true, expires: time.Now().Add(time.Second)}
		if exec.receiveLateReply(packet) {
			t.Fatal("superseded module accepted")
		}
	}
}

func TestExperimentalLateReplyDoesNotCompleteDifferentRequest(t *testing.T) {
	base := experimentalResponder()
	peer := newTestPeer(t, func(request []byte) [][]byte {
		if len(request) == 40 {
			return [][]byte{{7, 0, 6, 0, 6, 0}}
		} // Only an old ping; never the requested module.
		return base(request)
	})
	durations := defaultTestDurations()
	durations.keepalive = 15 * time.Millisecond
	run := experimentalTestRun(t, peer, durations)
	exec := newExecution(run.definition, nil, protocol.NoopAuthenticationProtocolDiagnostics{})
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.close()
	exec.exchange = ex
	exec.hasAuthInfo = true
	exec.experimental.interval = time.Second
	exec.experimental.lastEvidence = time.Now().Add(-time.Second)
	exec.experimental.ping = &pendingPing{late: true, expires: time.Now().Add(time.Second)}
	failure := exec.sendExperimentalModule(context.Background(), 1, phaseKeepaliveKA2)
	if failure == nil || failure.kind != kindNetworkTimeout || !exec.experimental.ping.received || exec.serial != 0 {
		t.Fatal("late ping completed module request")
	}
}

func TestExperimentalLatePingChallengeResumesWithoutBecomingOnlineEvidence(t *testing.T) {
	exec := replyTestExecution()
	initial := exec.experimental.lastEvidence
	flight := &pendingPingChallenge{serial: 7, late: true, expires: time.Now().Add(time.Second)}
	exec.experimental.pingChallenge = flight
	packet := make([]byte, 16)
	packet[0] = 7
	packet[1] = 7
	packet[2] = 16
	packet[4] = 2
	copy(packet[8:12], []byte{1, 2, 3, 4})
	if !exec.receiveLateReply(packet) || flight.reply == nil || exec.experimental.lastEvidence != initial {
		t.Fatal("challenge classification/evidence")
	}
	if exec.receiveLateReply(packet) {
		t.Fatal("duplicate challenge accepted")
	}
	peer := newTestPeer(t, experimentalResponder())
	run := experimentalTestRun(t, peer, defaultTestDurations())
	exec.definition = run.definition
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.close()
	exec.exchange = ex
	if failure := exec.sendChallengePing(context.Background(), phaseKeepaliveKA1); failure != nil {
		t.Fatal(failure)
	}
	requests := peer.requests()
	if len(requests) != 1 || !isKA1Req(requests[0]) || exec.experimental.pingChallenge != nil {
		t.Fatal("late challenge did not resume its original ping")
	}
}

func TestExperimentalLateMetadataIsBoundedAndNotOnlineEvidence(t *testing.T) {
	exec := replyTestExecution()
	initial := exec.experimental.lastEvidence
	flight := &pendingModule{serial: 2, typ: 1, nonce: [4]byte{1, 3, 5, 7}, late: true, expires: time.Now().Add(time.Second)}
	exec.experimental.module = flight
	packet := buildExperimentalModule(2, 6, 733, flight.nonce, [8]byte{}, [4]byte{})
	binary.LittleEndian.PutUint32(packet[16:20], 128)
	if !exec.receiveLateReply(packet) || exec.experimental.lastEvidence != initial {
		t.Fatal("metadata classification/evidence")
	}
	peer := newTestPeer(t, experimentalResponder())
	run := experimentalTestRun(t, peer, defaultTestDurations())
	exec.definition = run.definition
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.close()
	exec.exchange = ex
	if f := exec.sendExperimentalModule(context.Background(), 1, phaseKeepaliveKA2); f != nil {
		t.Fatal(f)
	}
	requests := peer.requests()
	if len(requests) != 1 || binary.LittleEndian.Uint32(requests[0][12:16]) != 40 || binary.LittleEndian.Uint16(requests[0][6:8]) != 733 {
		t.Fatal("late metadata was not consumed in module stage")
	}
	if exec.receiveLateReply(packet) {
		t.Fatal("old metadata affected newer stage")
	}
}

func TestExperimentalRejectionDetailsAndSafeFallback(t *testing.T) {
	rejection := func(code byte, n int) []byte { p := make([]byte, n); p[0] = 5; p[4] = code; return p }
	cases := []struct {
		name   string
		packet []byte
		code   string
	}{
		{"session-detail", rejection(1, 15), "session_in_use"}, {"partial-session", rejection(1, 14), "authentication_rejected"},
		{"quota", rejection(4, 5), "quota_exceeded"}, {"funds", rejection(4, 10), "insufficient_funds"},
		{"suspended", rejection(5, 5), "account_suspended"}, {"expired", rejection(5, 6), "account_expired"},
		{"bound-ip", rejection(7, 9), "binding_ip_mismatch"}, {"partial-ip", rejection(7, 8), "authentication_rejected"},
		{"bound-mac", rejection(0x0b, 11), "binding_mac_mismatch"}, {"partial-mac", rejection(0x0b, 10), "authentication_rejected"},
		{"addresses", rejection(0x14, 5), "too_many_addresses"}, {"version", rejection(0x15, 5), "incompatible_version"},
		{"empty-reason", append(rejection(0x15, 20), 0), "incompatible_version"},
		{"unknown", rejection(0xf0, 5), "authentication_rejected"},
	}
	const private = "synthetic-private-text-192.0.2.9-secret"
	cases = append(cases,
		struct {
			name   string
			packet []byte
			code   string
		}{"text", append(rejection(0x15, 20), append([]byte(private), 0)...), "authentication_rejected"},
		struct {
			name   string
			packet []byte
			code   string
		}{"unterminated", append(rejection(0x15, 20), []byte(private)...), "authentication_rejected"},
		struct {
			name   string
			packet []byte
			code   string
		}{"empty-with-url", append(rejection(0x15, 20), append([]byte{0}, append([]byte("https://example.invalid/private"), 0)...)...), "incompatible_version"},
	)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			failure := experimentalRejectionError(test.packet).toFailure()
			if string(failure.Code) != test.code || failure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
				t.Fatalf("%#v", failure)
			}
			if strings.Contains(fmt.Sprint(failure), private) || strings.Contains(fmt.Sprint(failure.DiagnosticCause), private) || strings.Contains(failure.Description, "https:") {
				t.Fatal("untrusted detail leaked")
			}
		})
	}
	// The original institution path retains its established byte-code behavior.
	if rejectionError("login", 4, nil).toFailure().Code != "insufficient_funds" || rejectionError("login", 5, nil).toFailure().Code != "account_frozen" {
		t.Fatal("legacy rejection changed")
	}
}

func TestExperimentalLoginUsesDetailClassification(t *testing.T) {
	base := experimentalResponder()
	peer := newTestPeer(t, func(request []byte) [][]byte {
		if isLoginReq(request) {
			return [][]byte{{5, 0, 0, 0, 4}}
		}
		return base(request)
	})
	run := experimentalTestRun(t, peer, defaultTestDurations())
	failure := run.Execute(context.Background(), newRecordingObserver(peer))
	if failure == nil || failure.Code != "quota_exceeded" {
		t.Fatalf("login details not used: %#v", failure)
	}
}

func TestExperimentalAdminResetAdviceDoesNotTrustText(t *testing.T) {
	for _, body := range []string{"AdminReset", "AdminReset>synthetic restriction", "AdminReset>"} {
		exec := replyTestExecution()
		packet := append([]byte{0x4d, 0x15, 0, 0}, body...)
		failure := exec.handleControl(context.Background(), packet).toFailure()
		if failure.Code != "server_session_reset" || failure.HandlingRecommendation != protocol.RetryAfterBoundedRecovery || !failure.RetainRecoveryBudget {
			t.Fatalf("%#v", failure)
		}
		if strings.Contains(failure.Description, "synthetic restriction") || strings.Contains(fmt.Sprint(failure.DiagnosticCause), body) {
			t.Fatal("server text used as policy or exposed")
		}
	}
	exec := replyTestExecution()
	packet := []byte{0x4d, 0x15, 0, 0}
	failure := exec.handleControl(context.Background(), packet).toFailure()
	if failure.Code != "server_session_ended" || failure.HandlingRecommendation != protocol.RetryAfterStandardDelay {
		t.Fatal("not-logged-in collapsed into reset")
	}
}

func TestExperimentalReplyExpiryDoesNotMoveWithNewEvidence(t *testing.T) {
	exec := replyTestExecution()
	expiry := exec.replyExpiry()
	exec.experimental.ping = &pendingPing{late: true, expires: expiry}
	// Other useful traffic may keep the session alive, but must not extend this
	// old request's own absolute acceptance deadline.
	exec.experimental.lastEvidence = time.Now().Add(time.Second)
	exec.experimental.interval = 10 * time.Second
	if exec.experimental.ping.expires != expiry {
		t.Fatal("request lifetime moved")
	}
}
