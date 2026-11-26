package driver

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/connection/auth/d520/internal/settings"
	protocol "sidravia/internal/connection/auth/protocol"
)

func TestFreshFixedPortRunRejectsLateKAAtChallengeThenCleanRunRecovers(t *testing.T) {
	reserved, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := reserved.LocalAddr().(*net.UDPAddr).Port
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	base := defaultPeerResponder()
	challenges := 0
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if isChallengeReq(req) {
			challenges++
			if challenges == 1 {
				return nil
			}
			if challenges == 2 {
				return [][]byte{peerKA2Response(7, ka2Type1, [4]byte{})}
			}
		}
		return base(req)
	})
	newRun := func() protocol.AuthenticationProtocolRun {
		r := buildTestRun(t, peer, defaultTestDurations(), testCredential()).(*d520Run)
		r.definition.cfg.LocalPort = settings.LocalPort{Mode: settings.LocalPortFixed, Value: uint16(port)}
		return r
	}
	for i, want := range []protocol.AuthenticationProtocolFailureHandlingRecommendation{protocol.RetryAfterStandardDelay, protocol.RetryAfterBoundedRecovery} {
		observer := newRecordingObserver(peer)
		_, cancel, done := startRun(t, newRun(), observer)
		failure := assertRunReturns(t, done, false, 2*time.Second)
		cancel(context.Canceled)
		if failure.HandlingRecommendation != want || observer.establishedCallCount() != 0 {
			t.Fatalf("run%d failure=%#v established=%d", i, failure, observer.establishedCallCount())
		}
	}
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, newRun(), observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
}

func TestRunBootstrapAndHeartbeat(t *testing.T) {
	peer := newTestPeer(t, defaultPeerResponder())
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	observer.waitForEstablished(t, time.Second)
	if observer.establishedCallCount() != 1 {
		t.Fatalf("established called %d times, want 1", observer.establishedCallCount())
	}
	if observer.establishedAtRequests < 6 {
		t.Fatalf("established fired after %d requests, want >= 6 (bootstrap complete)", observer.establishedAtRequests)
	}
	// Wait for bootstrap (6) + one heartbeat (KA1 + KA2 + KA2 = 3).
	waitForRequestCount(t, peer, 9, time.Second)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)

	ops := peer.requestOpNames()
	wantPrefix := []string{"challenge", "login", "ka1", "ka2", "ka2", "ka2", "ka1", "ka2", "ka2"}
	if !hasOpPrefix(ops, wantPrefix) {
		t.Fatalf("request order = %v, want prefix %v", ops, wantPrefix)
	}
}

// 2. busy responses retry within the Run, exhaustion maps to extended delay and
// a normal rejection blocks without observer notification.
func TestRunBusyRetry(t *testing.T) {
	var loginCount int
	authInfo := [16]byte{0x05, 0xbb, 0xfe, 0x85, 0xe2, 0x26, 0xf7, 0x35, 0xad, 0x5f, 0xc3, 0xab, 0x4d, 0xcc, 0x6e, 0x76}
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			loginCount++
			if loginCount == 1 {
				return [][]byte{peerLoginRejection(0x02)}
			}
			return [][]byte{peerLoginSuccess(authInfo)}
		case isKA1Req(req):
			return [][]byte{peerKA1Response()}
		case isKA2Req(req):
			return [][]byte{peerKA2Response(req[1], req[5], [4]byte{req[1], req[5], 0xaa, 0xbb})}
		case isLogoutReq(req):
			return [][]byte{peerLogoutACK()}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	observer.waitForEstablished(t, 2*time.Second)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	if countOps(peer.requestOpNames(), "login") != 2 {
		t.Fatalf("login count = %d, want 2 (busy then success)", countOps(peer.requestOpNames(), "login"))
	}
}

func TestRunBusyExhaustionMapsToExtendedDelay(t *testing.T) {
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			return [][]byte{peerLoginRejection(0x02)}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	failure := assertRunReturns(t, done, false, 2*time.Second)
	if failure.Code != "server_busy" || failure.HandlingRecommendation != protocol.RetryAfterExtendedDelay {
		t.Fatalf("exhaustion failure = code=%q recommendation=%q, want server_busy/extended", failure.Code, failure.HandlingRecommendation)
	}
	if observer.establishedCallCount() != 0 {
		t.Fatalf("observer called %d times, want 0", observer.establishedCallCount())
	}
	if countOps(peer.requestOpNames(), "login") != 3 {
		t.Fatalf("login count = %d, want 3 (busyMaxAttempts)", countOps(peer.requestOpNames(), "login"))
	}
}

func TestRunNormalRejectionBlocksWithoutObserver(t *testing.T) {
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			return [][]byte{peerLoginRejection(0x03)}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	failure := assertRunReturns(t, done, false, 2*time.Second)
	if failure.Code != "credential_invalid" || failure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
		t.Fatalf("rejection failure = code=%q recommendation=%q, want credential_invalid/block", failure.Code, failure.HandlingRecommendation)
	}
	if observer.establishedCallCount() != 0 {
		t.Fatalf("observer called %d times, want 0", observer.establishedCallCount())
	}
	if countOps(peer.requestOpNames(), "login") != 1 {
		t.Fatalf("login count = %d, want 1 (no retry for normal rejection)", countOps(peer.requestOpNames(), "login"))
	}
}

// 3. Challenge or heartbeat timeout maps to standard delay.
func TestRunChallengeTimeoutMapsToStandardDelay(t *testing.T) {
	d := defaultTestDurations()
	d.challenge = 50 * time.Millisecond
	respond := func(req []byte) [][]byte {
		// Drop every request so the Challenge exchange times out.
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, d, testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	failure := assertRunReturns(t, done, false, 2*time.Second)
	if failure.Code != "network_timeout" || failure.HandlingRecommendation != protocol.RetryAfterStandardDelay {
		t.Fatalf("challenge timeout failure = code=%q recommendation=%q, want network_timeout/standard", failure.Code, failure.HandlingRecommendation)
	}
	if observer.establishedCallCount() != 0 {
		t.Fatalf("observer called %d times, want 0", observer.establishedCallCount())
	}
}

func TestRunHeartbeatTimeoutMapsToStandardDelay(t *testing.T) {
	d := defaultTestDurations()
	d.keepalive = 50 * time.Millisecond
	var ka1Count int
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess([16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10})}
		case isKA1Req(req):
			ka1Count++
			if ka1Count == 1 {
				return [][]byte{peerKA1Response()}
			}
			return nil // drop heartbeat KA1 -> heartbeat timeout
		case isKA2Req(req):
			return [][]byte{peerKA2Response(req[1], req[5], [4]byte{req[1], req[5], 0xaa, 0xbb})}
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
	failure := assertRunReturns(t, done, false, 2*time.Second)
	if failure.Code != "network_timeout" || failure.HandlingRecommendation != protocol.RetryAfterStandardDelay {
		t.Fatalf("heartbeat timeout failure = code=%q recommendation=%q, want network_timeout/standard", failure.Code, failure.HandlingRecommendation)
	}
}

// Malformed responses fail strictly and recommend bounded full recovery.
func TestRunMalformedResponseRecommendsBoundedRecovery(t *testing.T) {
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			// Malformed: a short, unidentifiable datagram.
			return [][]byte{{0x09, 0x00, 0x00}}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	failure := assertRunReturns(t, done, false, 2*time.Second)
	if failure.Code != "protocol_response_invalid" || failure.HandlingRecommendation != protocol.RetryAfterBoundedRecovery {
		t.Fatalf("malformed failure = code=%q recommendation=%q, want protocol_response_invalid/bounded", failure.Code, failure.HandlingRecommendation)
	}
	if observer.establishedCallCount() != 0 {
		t.Fatalf("observer called %d times, want 0", observer.establishedCallCount())
	}
}

// 5. a recognized late KA response is ignored without extending the exchange
// deadline or changing state.
func TestRunIgnoresLateKAResponse(t *testing.T) {
	var ka1Count int
	authInfo := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess(authInfo)}
		case isKA1Req(req):
			ka1Count++
			if ka1Count == 1 {
				return [][]byte{peerKA1Response()}
			}
			// Heartbeat KA1: inject a stale KA2 (serial 0, type 1, already sent
			// during bootstrap) before the real KA1 response.
			stale := peerKA2Response(0, ka2Type1, [4]byte{0x00, 0x01, 0xaa, 0xbb})
			return [][]byte{stale, peerKA1Response()}
		case isKA2Req(req):
			return [][]byte{peerKA2Response(req[1], req[5], [4]byte{req[1], req[5], 0xaa, 0xbb})}
		case isLogoutReq(req):
			return [][]byte{peerLogoutACK()}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	observer.waitForEstablished(t, time.Second)
	// Bootstrap (6) + heartbeat KA1 + heartbeat KA2 + heartbeat KA2.
	waitForRequestCount(t, peer, 9, time.Second)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)

	if observer.establishedCallCount() != 1 {
		t.Fatalf("established called %d times, want 1", observer.establishedCallCount())
	}
	serials := ka2RequestSerials(peer.requests())
	if !equalBytes(serials, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("ka2 request serials = %v, want [0 1 2 3 4] (stale response did not advance state)", serials)
	}
}

// 6. cancellation wakes an in-flight read promptly, preserves the socket for
// fresh-salt Logout, and returns nil.
func TestTestPeerShutdownClosesSocketAndAwaitsGoroutine(t *testing.T) {
	peer := newTestPeer(t, defaultPeerResponder())
	peer.shutdown()
	if !peer.stopped.Load() {
		t.Fatal("serve goroutine did not exit after shutdown")
	}
	// The listener is closed: a further write fails.
	if _, err := peer.conn.WriteToUDP([]byte{0x00}, peer.addr); err == nil {
		t.Fatal("write to closed peer socket unexpectedly succeeded")
	}
}

// 11. the complete bootstrap accepts the observed campus response variants
// (extended Challenge, extended Login success, extended KA1, KA2 response
// types 6 -> 2 -> 4 with extensions), notifies AuthenticationEstablished
// exactly once, and retains cancellation with best-effort Logout cleanup
// against an extended Logout response.
func TestRunAcceptsCampusResponseVariants(t *testing.T) {
	authInfo := [16]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf}
	type2Tail := [4]byte{0x21, 0x22, 0x23, 0x24}
	type4Tail := [4]byte{0x41, 0x42, 0x43, 0x44}
	var ka1Count int
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponseVariant([4]byte{0x01, 0x02, 0x03, 0x04}, 76)}
		case isLoginReq(req):
			return [][]byte{peerLoginSuccessVariant(authInfo, 45)}
		case isKA1Req(req):
			ka1Count++
			if ka1Count == 3 {
				// The third KA1 begins the second heartbeat. Leave its read
				// unanswered so cancellation starts from a deterministic
				// point with no late heartbeat response in the socket.
				return nil
			}
			return [][]byte{peerKA1ResponseVariant(72)}
		case isKA2Req(req):
			serial, reqType := req[1], req[5]
			switch {
			case serial == 0:
				// First bootstrap Type1: a fictional extended special-frame
				// Type 6 acknowledges without refilling Tail.
				return [][]byte{peerKA2BootstrapType6Response(serial, 272)}
			case reqType == ka2Type1:
				return [][]byte{peerKA2ResponseVariant(serial, 2, type2Tail, 40)}
			default:
				return [][]byte{peerKA2ResponseVariant(serial, 4, type4Tail, 40)}
			}
		case isLogoutReq(req):
			return [][]byte{peerLogoutResponseVariant(25)}
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	// An early return means a variant was rejected; establishment means the
	// whole KA1 -> type 6 -> type 2 -> type 4 bootstrap was accepted.
	select {
	case failure := <-done:
		if failure == nil {
			t.Fatal("run returned before cancellation")
		}
		t.Fatalf("run returned early failure: code=%q description=%q", failure.Code, failure.Description)
	case <-observer.established:
	case <-time.After(2 * time.Second):
		t.Fatal("authentication was not established with campus response variants")
	}
	if observer.establishedCallCount() != 1 {
		t.Fatalf("established called %d times, want 1", observer.establishedCallCount())
	}
	// Bootstrap (6) + one heartbeat (KA1 + KA2 + KA2) with the same variants.
	waitForRequestCount(t, peer, 9, time.Second)
	if serials := ka2RequestSerials(peer.requests()); !equalBytes(serials, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("ka2 request serials = %v, want [0 1 2 3 4]", serials)
	}
	ka2 := ka2Requests(peer.requests())
	zeroTail := [4]byte{}
	var firstTail, secondTail, bootstrapType3Tail, heartbeatType1Tail [4]byte
	copy(firstTail[:], ka2[0][16:20])
	copy(secondTail[:], ka2[1][16:20])
	copy(bootstrapType3Tail[:], ka2[2][16:20])
	copy(heartbeatType1Tail[:], ka2[3][16:20])
	if firstTail != zeroTail {
		t.Fatalf("first bootstrap Type1 Tail = %x, want zero", firstTail)
	}
	if secondTail != zeroTail {
		t.Fatalf("second bootstrap Type1 Tail = %x, want zero after Type 6", secondTail)
	}
	if bootstrapType3Tail != type2Tail {
		t.Fatalf("bootstrap Type3 Tail = %x, want Type 2 refill %x", bootstrapType3Tail, type2Tail)
	}
	if heartbeatType1Tail != type4Tail {
		t.Fatalf("heartbeat Type1 Tail = %x, want Type 4 refill %x", heartbeatType1Tail, type4Tail)
	}

	// Receiving the third KA1 proves the Run consumed the complete first
	// heartbeat response. Its deliberately unanswered read is then cancelled.
	waitForRequestCount(t, peer, 10, time.Second)

	cancel(protocol.AuthenticationProtocolRunCancellationCause{
		CleanupRequirement: protocol.TerminateWithBestEffortLogout,
		Description:        "test cancellation",
	})
	assertRunReturns(t, done, true, 2*time.Second)
	waitForOp(t, peer, "logout", time.Second)
	ops := peer.requestOpNames()
	if countOps(ops, "challenge") != 2 {
		t.Fatalf("expected login challenge + fresh logout challenge: ops=%v", ops)
	}
	if countOps(ops, "logout") != 1 {
		t.Fatalf("expected one logout after cancellation: ops=%v", ops)
	}
	if observer.establishedCallCount() != 1 {
		t.Fatalf("established called %d times, want 1 after cleanup", observer.establishedCallCount())
	}
}

func TestRunExpectedBootstrapType6DoesNotBecomeNetworkTimeout(t *testing.T) {
	response := peerKA2BootstrapType6Response(0, 20)
	exec := newExecution(runDefinition{}, nil, protocol.NoopAuthenticationProtocolDiagnostics{})
	if _, err := exec.classifyKA2Response(0, ka2Type1, false)(response); err == nil {
		t.Fatal("non-bootstrap serial-zero Type1 accepted Type6")
	}
	if got, err := exec.classifyKA2Response(0, ka2Type1, true)(response); err != nil || got != responseAccept {
		t.Fatalf("expected special Type 6 classifier result = %d, err %v; want accept (not stale KA1 timeout)", got, err)
	}
}

// TestBackoffProjectionIncludesClosedIntervalEndpoints proves the deterministic
// backoff projection can return both endpoints of the closed interval [min,
// max], which the previous modulo span excluded.
func TestBackoffProjectionIncludesClosedIntervalEndpoints(t *testing.T) {
	cases := []struct {
		name     string
		sample   uint64
		min, max time.Duration
		want     time.Duration
	}{
		{"equal endpoints return that endpoint", 0, 5 * time.Second, 5 * time.Second, 5 * time.Second},
		{"sample zero returns min", 0, 1 * time.Second, 2 * time.Second, 1 * time.Second},
		{"max==min+1ns sample one returns max", 1, 1 * time.Second, 1*time.Second + 1*time.Nanosecond, 1*time.Second + 1*time.Nanosecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectBackoff(tc.sample, tc.min, tc.max); got != tc.want {
				t.Fatalf("projectBackoff(sample=%d, min=%s, max=%s) = %s, want %s", tc.sample, tc.min, tc.max, got, tc.want)
			}
		})
	}
}

// TestChallengeSeedProjection proves the pure Challenge seed projection returns
// the documented (timestamp_seconds + random_offset) % 0xFFFF value at both
// inclusive offset endpoints (0x0f and 0xff), wraps around 0xFFFF, and is
// encoded little-endian in the built request. The sampled offset always stays
// inside [0x0f, 0xff].
func TestChallengeSeedProjection(t *testing.T) {
	cases := []struct {
		name      string
		timestamp int64
		offset    int
		want      uint16
	}{
		{"min offset 0x0f at zero timestamp", 0, 0x0f, 0x000f},
		{"max offset 0xff at zero timestamp", 0, 0xff, 0x00ff},
		{"timestamp 0xFFFF wraps to zero mod 0xFFFF", 0xFFFF, 0, 0x0000},
		{"offset 0xff crosses 0xFFFF boundary", 0xFFFA, 0xff, 0x00fa},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectChallengeSeed(tc.timestamp, tc.offset); got != tc.want {
				t.Fatalf("projectChallengeSeed(%d, 0x%x) = 0x%04x, want 0x%04x", tc.timestamp, tc.offset, got, tc.want)
			}
		})
	}

	for i := 0; i < 256; i++ {
		offset := randomChallengeSeedOffset()
		if offset < challengeSeedOffsetMin || offset > challengeSeedOffsetMax {
			t.Fatalf("randomChallengeSeedOffset = 0x%x, want in [0x%x, 0x%x]", offset, challengeSeedOffsetMin, challengeSeedOffsetMax)
		}
	}

	seed := projectChallengeSeed(0, 0x0f) // 0x000f
	req := buildChallengeRequest(seed, [15]byte{})
	if req[2] != 0x0f || req[3] != 0x00 {
		t.Fatalf("challenge request seed bytes = %x %x, want 0f 00 (little-endian)", req[2], req[3])
	}
}

// TestFactoryPropagatesLocalPortAndPaddingIntoRun proves the decoded localPort
// and the three Login padding fields propagate from the Profile through the
// factory into the private run definition, without a public production seam.
func TestFactoryPropagatesLocalPortAndPaddingIntoRun(t *testing.T) {
	cfg := configWithOverride(t, map[string]any{
		"localPort":                    map[string]any{"mode": "fixed", "value": 61440},
		"loginIPDogPaddingHex":         "11223344",
		"loginDHCPPaddingHex":          "1122334455667788",
		"loginAuthExtensionPaddingHex": "aabb",
	})
	run, err := NewFactory().CreateAuthenticationProtocolRun(factoryInputs(cfg, testCredential(), testBinding(t)))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	d520, ok := run.(*d520Run)
	if !ok {
		t.Fatalf("run type = %T, want *d520Run", run)
	}
	if d520.definition.cfg.LocalPort.Mode != settings.LocalPortFixed || d520.definition.cfg.LocalPort.Value != 61440 {
		t.Fatalf("localPort = mode %d value %d, want fixed/61440", d520.definition.cfg.LocalPort.Mode, d520.definition.cfg.LocalPort.Value)
	}
	if d520.definition.login.loginIPDogPadding != [4]byte{0x11, 0x22, 0x33, 0x44} {
		t.Fatalf("loginIPDogPadding = %x, want 11223344", d520.definition.login.loginIPDogPadding)
	}
	if d520.definition.login.loginDHCPPadding != [8]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88} {
		t.Fatalf("loginDHCPPadding = %x, want 1122334455667788", d520.definition.login.loginDHCPPadding)
	}
	if d520.definition.login.loginAuthExtensionPadding != [2]byte{0xaa, 0xbb} {
		t.Fatalf("loginAuthExtensionPadding = %x, want aabb", d520.definition.login.loginAuthExtensionPadding)
	}
}

// This fixture uses the existing Go-owned loopback responder and observer, but
// records ReadFromUDP's source address alongside the existing wire requests.
// It sends no traffic beyond 127.0.0.1 and owns and awaits its server goroutine.
func TestRunReportedIPv4IsIndependentFromActualSocketSource(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	type receivedDatagram struct {
		source netip.AddrPort
		packet []byte
	}
	received := make(chan receivedDatagram, 32)
	stopped := make(chan struct{})
	respond := defaultPeerResponder()
	go func() {
		defer close(stopped)
		buf := make([]byte, 1500)
		for {
			n, source, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			packet := bytes.Clone(buf[:n])
			select {
			case received <- receivedDatagram{source, packet}:
			default:
				return
			}
			for _, response := range respond(packet) {
				if _, err := conn.WriteToUDPAddrPort(response, source); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() { conn.Close(); <-stopped })
	inputs := factoryInputs(testConfigJSON(conn.LocalAddr().(*net.UDPAddr).Port, defaultTestDurations()), testCredential(), buildBinding(t, "127.0.0.1", nil, nil, nil))
	inputs.ProtocolContextOverride = []byte(`{"schemaVersion":1,"reportedIPv4":"192.0.2.42","reportedMAC":"02:00:00:00:00:01"}`)
	run, err := NewFactory().CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatal(err)
	}
	// After creation, even replacing binding/override buffers cannot change
	// either the selected real source or the effective reported wire context.
	inputs.SelectedSystemNetworkBinding = buildBinding(t, "127.0.0.2", nil, nil, nil)
	inputs.ProtocolContextOverride[0] = 0
	observer := &socketRecordingObserver{recordingObserver: newRecordingObserver(nil)}
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	if len(observer.opened) != 1 || len(observer.closed) != 1 || !observer.closed[0] {
		t.Fatalf("socket observations: %#v / %v", observer.opened, observer.closed)
	}
	actual := observer.opened[0]
	wantRemote := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(conn.LocalAddr().(*net.UDPAddr).Port))
	if actual.remote != wantRemote || actual.local.Addr() != netip.MustParseAddr("127.0.0.1") || actual.local.Port() == 0 {
		t.Fatalf("actual endpoints: %#v", actual)
	}
	assertSocketPortReleased(t, actual.local)
	var source netip.AddrPort
	sawLogin, sawType3 := false, false
	for len(received) > 0 {
		datagram := <-received
		if datagram.source.Addr() != netip.MustParseAddr("127.0.0.1") || datagram.source.Port() == 0 {
			t.Fatalf("unexpected actual socket source %v", datagram.source)
		}
		if source.IsValid() && datagram.source != source {
			t.Fatal("Run changed socket source")
		}
		if datagram.source != actual.local {
			t.Fatalf("observed local %v differs from real source %v", actual.local, datagram.source)
		}
		source = datagram.source
		packet := datagram.packet
		switch {
		case isLoginReq(packet):
			sawLogin = true
			if !bytes.Equal(packet[81:85], []byte{192, 0, 2, 42}) || !bytes.Equal(packet[97:105], md5C([4]byte{192, 0, 2, 42})) {
				t.Fatal("Login or MD5-C used actual source instead of reported IP")
			}
		case isKA2Req(packet) && packet[5] == ka2Type3:
			sawType3 = true
			if !bytes.Equal(packet[28:32], []byte{192, 0, 2, 42}) {
				t.Fatal("KA2 used actual source instead of reported IP")
			}
		}
	}
	if !sawLogin || !sawType3 || !source.IsValid() {
		t.Fatal("missing observed source/Login/KA2 evidence")
	}
}
