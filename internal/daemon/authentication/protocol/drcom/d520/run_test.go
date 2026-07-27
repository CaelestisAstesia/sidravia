package d520

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
	credential "sidravia/internal/daemon/credentials"
	environment "sidravia/internal/daemon/environment"
)

// testDurations holds the Profile timeouts used by run tests, kept small so
// failures and heartbeats are observed quickly.
type testDurations struct {
	challenge, login, keepalive, logout, heartbeat, backoffMin, backoffMax time.Duration
}

func defaultTestDurations() testDurations {
	return testDurations{
		challenge:  200 * time.Millisecond,
		login:      200 * time.Millisecond,
		keepalive:  200 * time.Millisecond,
		logout:     100 * time.Millisecond,
		heartbeat:  40 * time.Millisecond,
		backoffMin: 5 * time.Millisecond,
		backoffMax: 10 * time.Millisecond,
	}
}

func testConfigJSON(port int, d testDurations) protocol.InstitutionProtocolConfiguration {
	jsonStr := fmt.Sprintf(`{
        "serverAddress": "127.0.0.1",
        "serverPort": %d,
        "authVersionHex": "2c00",
        "keepAliveVersionHex": "dc02",
        "controlCheckStatusHex": "20",
        "ipdogHex": "01",
        "adapterNumberHex": "01",
        "osInfoHex": "940000000600000000000000280a000002000000",
        "challengePaddingHex": "000000000000000000000000000000",
        "challengeTimeout": "%s",
        "loginTimeout": "%s",
        "keepaliveTimeout": "%s",
        "logoutTimeout": "%s",
        "heartbeatInterval": "%s",
        "busyMaxAttempts": 3,
        "busyBackoffMin": "%s",
        "busyBackoffMax": "%s"
    }`, port, d.challenge, d.login, d.keepalive, d.logout, d.heartbeat, d.backoffMin, d.backoffMax)
	return protocol.InstitutionProtocolConfiguration(jsonStr)
}

func testBinding(t *testing.T) environment.SelectedSystemNetworkBinding {
	t.Helper()
	dhcp := netip.MustParseAddr("127.0.0.1")
	networkInterface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:             "test",
		DisplayName:             "test",
		OperationalState:        environment.OperationalStateUp,
		PhysicalMedium:          environment.PhysicalMediumWired,
		AddressAssignmentMethod: environment.AddressAssignmentDHCP,
		HardwareAddress:         []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("127.0.0.1"), PrefixLength: 8,
		}},
		DNSServerAddresses:    []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")},
		DHCPServerIPv4Address: &dhcp,
	})
	if err != nil {
		t.Fatalf("new network interface: %v", err)
	}
	binding, err := environment.NewSelectedSystemNetworkBinding(networkInterface, environment.IPv4AddressAssignment{
		Address: netip.MustParseAddr("127.0.0.1"), PrefixLength: 8,
	})
	if err != nil {
		t.Fatalf("new binding: %v", err)
	}
	return binding
}

func testHostInformation() environment.SystemHostInformation {
	return environment.SystemHostInformation{
		HostName:               "local-test-host",
		OperatingSystemFamily:  "Windows",
		OperatingSystemRelease: "10",
		MachineArchitecture:    "x86_64",
	}
}

func testCredential() credential.AuthenticationCredential {
	return credential.AuthenticationCredential{
		Username: "student-test",
		Password: "local-test-password",
	}
}

func buildTestRun(t *testing.T, peer *testPeer, d testDurations, cred credential.AuthenticationCredential) protocol.AuthenticationProtocolRun {
	t.Helper()
	factory := NewFactory()
	inputs := protocol.AuthenticationProtocolRunCreationInputs{
		InstitutionProtocolConfiguration: testConfigJSON(peer.port(), d),
		AuthenticationCredential:         cred,
		SelectedSystemNetworkBinding:     testBinding(t),
		SystemHostInformation:            testHostInformation(),
		ProtocolContextOverride:          nil,
	}
	run, err := factory.CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run
}

func startRun(t *testing.T, run protocol.AuthenticationProtocolRun, observer protocol.AuthenticationProtocolRunObserver) (context.Context, context.CancelCauseFunc, <-chan *protocol.AuthenticationProtocolRunFailure) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan *protocol.AuthenticationProtocolRunFailure, 1)
	go func() { done <- run.Execute(ctx, observer) }()
	return ctx, cancel, done
}

func assertRunReturns(t *testing.T, done <-chan *protocol.AuthenticationProtocolRunFailure, wantNil bool, timeout time.Duration) *protocol.AuthenticationProtocolRunFailure {
	t.Helper()
	select {
	case failure := <-done:
		if wantNil && failure != nil {
			t.Fatalf("expected nil failure, got code=%q description=%q recommendation=%q", failure.Code, failure.Description, failure.HandlingRecommendation)
		}
		if !wantNil && failure == nil {
			t.Fatal("expected non-nil failure, got nil")
		}
		return failure
	case <-time.After(timeout):
		t.Fatalf("run did not return within %s", timeout)
		return nil
	}
}

func waitForRequestCount(t *testing.T, peer *testPeer, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(peer.requests()) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("peer received %d requests, want >= %d: ops=%v", len(peer.requests()), want, peer.requestOpNames())
}

func waitForOp(t *testing.T, peer *testPeer, op string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if countOps(peer.requestOpNames(), op) > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("peer did not receive %q within %s: ops=%v", op, timeout, peer.requestOpNames())
}

func hasOpPrefix(ops, prefix []string) bool {
	if len(ops) < len(prefix) {
		return false
	}
	for i, op := range prefix {
		if ops[i] != op {
			return false
		}
	}
	return true
}

func countOps(ops []string, op string) int {
	count := 0
	for _, o := range ops {
		if o == op {
			count++
		}
	}
	return count
}

func ka2RequestSerials(requests [][]byte) []byte {
	out := make([]byte, 0, len(requests))
	for _, req := range requests {
		if isKA2Req(req) {
			out = append(out, req[1])
		}
	}
	return out
}

func ka2Requests(requests [][]byte) [][]byte {
	var out [][]byte
	for _, req := range requests {
		if isKA2Req(req) {
			out = append(out, req)
		}
	}
	return out
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 1. successful Challenge/Login/bootstrap has request order KA1 + 1-1-3, calls
// observer only after bootstrap, then performs one KA1 + 1-3 heartbeat.
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

// 4. malformed response blocks.
func TestRunMalformedResponseBlocks(t *testing.T) {
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
	if failure.Code != "protocol_response_invalid" || failure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
		t.Fatalf("malformed failure = code=%q recommendation=%q, want protocol_response_invalid/block", failure.Code, failure.HandlingRecommendation)
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
func TestRunFallbackLogoutUsesLoginSalt(t *testing.T) {
	loginSalt := [4]byte{0x01, 0x02, 0x03, 0x04}
	var challengeCount int
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			challengeCount++
			if challengeCount == 1 {
				return [][]byte{peerChallengeResponse(loginSalt)}
			}
			// Fresh logout challenge: drop it so the Run falls back.
			return nil
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess([16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10})}
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

	observer.waitForEstablished(t, time.Second)
	cancel(protocol.AuthenticationProtocolRunCancellationCause{
		CleanupRequirement: protocol.TerminateWithBestEffortLogout,
		Description:        "test cancellation",
	})
	assertRunReturns(t, done, true, 2*time.Second)
	waitForOp(t, peer, "logout", time.Second)

	ops := peer.requestOpNames()
	if countOps(ops, "challenge") != 2 {
		t.Fatalf("expected login challenge + failed fresh challenge: ops=%v", ops)
	}
	if countOps(ops, "logout") != 1 {
		t.Fatalf("expected one fallback logout: ops=%v", ops)
	}
	// The fallback Logout must use the login salt. Recompute MD5-A with the
	// login salt and compare to the Logout request's [4,20) region.
	var logoutReq []byte
	for _, req := range peer.requests() {
		if isLogoutReq(req) {
			logoutReq = req
		}
	}
	if logoutReq == nil {
		t.Fatal("no logout request recorded")
	}
	expectedMD5A := md5A(loginSalt[:], []byte(testCredential().Password))
	if !equalBytes(logoutReq[4:20], expectedMD5A) {
		t.Fatalf("fallback logout MD5-A = %x, want login-salt MD5-A %x", logoutReq[4:20], expectedMD5A)
	}
}

func TestRunCancellationReportsBestEffortLogoutFailure(t *testing.T) {
	d := defaultTestDurations()
	d.challenge = 20 * time.Millisecond
	d.logout = 20 * time.Millisecond
	var challengeCount int
	respond := func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			challengeCount++
			if challengeCount == 1 {
				return [][]byte{peerChallengeResponse([4]byte{0x01, 0x02, 0x03, 0x04})}
			}
			return nil
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess([16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10})}
		case isKA1Req(req):
			return [][]byte{peerKA1Response()}
		case isKA2Req(req):
			return [][]byte{peerKA2Response(req[1], req[5], [4]byte{req[1], req[5], 0xaa, 0xbb})}
		case isLogoutReq(req):
			return nil
		}
		return nil
	}
	peer := newTestPeer(t, respond)
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, d, testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	observer.waitForEstablished(t, time.Second)
	cancel(protocol.AuthenticationProtocolRunCancellationCause{
		CleanupRequirement: protocol.TerminateWithBestEffortLogout,
		Description:        "test cancellation",
	})
	failure := assertRunReturns(t, done, false, time.Second)
	if failure.Code != "logout_cleanup_failed" {
		t.Fatalf("cleanup failure Code = %q, want %q", failure.Code, "logout_cleanup_failed")
	}
	if failure.Description != "Best-effort logout cleanup failed." {
		t.Fatalf("cleanup failure Description = %q", failure.Description)
	}
	if failure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
		t.Fatalf("cleanup failure recommendation = %q, want block", failure.HandlingRecommendation)
	}
	if failure.DiagnosticCause == nil {
		t.Fatal("cleanup failure DiagnosticCause = nil")
	}
	if countOps(peer.requestOpNames(), "logout") != 1 {
		t.Fatalf("best-effort cleanup logout count = %d, want 1: ops=%v", countOps(peer.requestOpNames(), "logout"), peer.requestOpNames())
	}
}

// 8. TerminateWithoutLogout sends no Logout.
func TestRunTerminateWithoutLogoutSendsNoLogout(t *testing.T) {
	peer := newTestPeer(t, defaultPeerResponder())
	observer := newRecordingObserver(peer)
	run := buildTestRun(t, peer, defaultTestDurations(), testCredential())
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)

	observer.waitForEstablished(t, time.Second)
	cancel(protocol.AuthenticationProtocolRunCancellationCause{
		CleanupRequirement: protocol.TerminateWithoutLogout,
		Description:        "test cancellation",
	})
	assertRunReturns(t, done, true, 2*time.Second)

	// Give cleanup a moment to prove no Logout sneaks in.
	time.Sleep(50 * time.Millisecond)
	ops := peer.requestOpNames()
	if countOps(ops, "logout") != 0 {
		t.Fatalf("TerminateWithoutLogout sent a logout: ops=%v", ops)
	}
	if countOps(ops, "challenge") != 1 {
		t.Fatalf("TerminateWithoutLogout sent an extra challenge: ops=%v", ops)
	}
}

// 9. post-login heartbeat failure attempts Logout but returns the original
// failure code/recommendation.
func TestRunPostLoginHeartbeatFailureAttemptsLogout(t *testing.T) {
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
			// Heartbeat KA1: malformed unidentifiable response (wrong opcode
			// for every phase).
			return [][]byte{{0x08, 0x00, 0x00}}
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
	failure := assertRunReturns(t, done, false, 2*time.Second)
	if failure.Code != "protocol_response_invalid" || failure.HandlingRecommendation != protocol.BlockUntilExplicitRestartOrRelevantInputChange {
		t.Fatalf("post-login failure = code=%q recommendation=%q, want protocol_response_invalid/block", failure.Code, failure.HandlingRecommendation)
	}
	if failure.DiagnosticCause == nil {
		t.Fatal("expected a diagnostic cause on the original failure")
	}
	waitForOp(t, peer, "logout", time.Second)
	if countOps(peer.requestOpNames(), "logout") != 1 {
		t.Fatalf("expected one best-effort logout after post-login failure: ops=%v", peer.requestOpNames())
	}
}

// 10. all test peer goroutines and sockets are explicitly cancelled, closed
// and awaited.
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
	exec := newExecution(runDefinition{}, nil)
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
