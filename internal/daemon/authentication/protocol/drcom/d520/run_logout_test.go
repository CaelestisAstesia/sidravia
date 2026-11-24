package d520

import (
	"context"
	"testing"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

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
	if failure.Code != "protocol_response_invalid" || failure.HandlingRecommendation != protocol.RetryAfterBoundedRecovery {
		t.Fatalf("post-login failure = code=%q recommendation=%q, want protocol_response_invalid/bounded", failure.Code, failure.HandlingRecommendation)
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
