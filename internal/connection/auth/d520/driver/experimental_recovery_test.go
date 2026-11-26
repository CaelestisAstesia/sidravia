package driver

import (
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/connection/auth/protocol"
)

type recoveryDecisions struct {
	protocol.NoopAuthenticationProtocolDiagnostics
	mu       sync.Mutex
	timeouts []int
}

func (d *recoveryDecisions) DecisionEvent(name string, value int) {
	if name != "online_recovery_consecutive_timeouts" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.timeouts = append(d.timeouts, value)
}
func (d *recoveryDecisions) values() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int(nil), d.timeouts...)
}

// Only synthetic loopback traffic. Atomic flags separate completed bootstrap
// from the online fault policy; the peer itself owns its one-time metadata.
type delayedRecoveryPeer struct {
	online     atomic.Bool
	challenges atomic.Int32
	type3      atomic.Int32
}

func (s *delayedRecoveryPeer) responder(mode string) func([]byte) [][]byte {
	base := experimentalResponder()
	metadataSent := false
	var oldMetadata []byte
	var oldChallenge []byte
	return func(request []byte) [][]byte {
		var responses [][]byte
		if len(request) == 8 && request[0] == 7 {
			p := make([]byte, 16)
			p[0] = 7
			p[1] = request[1]
			p[2] = 16
			p[4] = 2
			copy(p[8:12], []byte{1, 2, 3, 4})
			responses = [][]byte{p}
		} else {
			responses = base(request)
		}
		if len(request) == 20 && request[0] == 1 {
			responses[0][43] = 0x80
		}
		if !s.online.Load() {
			return responses
		}
		keepalive := isKA1Req(request) || (request[0] == 7 && (len(request) == 8 || len(request) == 40))
		if !keepalive {
			return responses
		}
		if mode == "silent" {
			return nil
		}
		if len(request) == 8 {
			s.challenges.Add(1)
			oldChallenge = append([]byte(nil), responses[0]...)
		}
		if mode == "challenge-only" && isKA1Req(request) {
			return [][]byte{oldChallenge, {7, 0, 6, 0, 99, 0}, {0x4d, 0x38, 0, 0}}
		}
		if len(request) == 40 && request[5] == 1 && !metadataSent {
			metadataSent = true
			p := append([]byte(nil), responses[0]...)
			p[5] = 6
			binary.LittleEndian.PutUint16(p[6:8], 733)
			binary.LittleEndian.PutUint32(p[16:20], 128)
			responses = [][]byte{p}
			oldMetadata = append([]byte(nil), p...)
		}
		if len(request) == 40 && request[5] == 3 {
			s.type3.Add(1)
			if mode == "no-evidence" {
				// This metadata belongs to an already consumed request. Neither it nor
				// notification/unknown traffic may reset failures or refresh evidence.
				return [][]byte{oldMetadata, {7, 0, 6, 0, 99, 0}, {0x4d, 0x38, 0, 0}}
			}
		}
		time.Sleep(20 * time.Millisecond)
		return responses
	}
}

func TestExperimentalRecoveryProgressWithChallengeAndLateMetadata(t *testing.T) {
	policy := &delayedRecoveryPeer{}
	peer := newTestPeer(t, policy.responder("delayed"))
	durations := defaultTestDurations()
	durations.keepalive = 10 * time.Millisecond
	durations.heartbeat = 30 * time.Millisecond
	run := experimentalTestRun(t, peer, durations)
	diagnostics := &recoveryDecisions{}
	run.diagnostics = diagnostics
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	policy.online.Store(true)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	// A new challenge after three Type3 requests proves three full online
	// exchanges were consumed, not merely sent or temporarily still pending.
	for policy.type3.Load() < 3 || policy.challenges.Load() < 4 {
		select {
		case failure := <-done:
			t.Fatalf("progress exhausted recovery before later cycles: %#v", failure)
		case <-timer.C:
			t.Fatalf("later stages not completed: Type3=%d challenges=%d", policy.type3.Load(), policy.challenges.Load())
		case <-ticker.C:
		}
	}
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	values := diagnostics.values()
	failures := 0
	for _, value := range values {
		if value > 1 {
			t.Fatalf("successful consumed progress did not reset timeouts: %v", values)
		}
		if value == 1 {
			failures++
		}
	}
	if failures < 5 {
		t.Fatalf("combined fault path not exercised: %v", values)
	}
	if observer.establishedCallCount() != 1 {
		t.Fatal("recovery created another authentication")
	}
}

func TestExperimentalRecoveryStillExpiresWithoutOnlineEvidence(t *testing.T) {
	for _, mode := range []string{"silent", "challenge-only", "no-evidence"} {
		t.Run(mode, func(t *testing.T) {
			policy := &delayedRecoveryPeer{}
			peer := newTestPeer(t, policy.responder(mode))
			durations := defaultTestDurations()
			// Preserve request/evidence ratios with headroom for hosted scheduling.
			durations.keepalive = 50 * time.Millisecond
			durations.heartbeat = 150 * time.Millisecond
			durations.logout = time.Millisecond
			run := experimentalTestRun(t, peer, durations)
			diagnostics := &recoveryDecisions{}
			run.diagnostics = diagnostics
			observer := newRecordingObserver(peer)
			_, cancel, done := startRun(t, run, observer)
			defer cancel(context.Canceled)
			observer.waitForEstablished(t, 3*time.Second)
			policy.online.Store(true)
			failure := assertRunReturns(t, done, false, 3*time.Second)
			cause, ok := failure.DiagnosticCause.(*runError)
			if !ok || cause.operation != "online evidence expired" {
				t.Fatalf("expected total evidence deadline, got %#v", failure.DiagnosticCause)
			}
			values := diagnostics.values()
			maximum := 0
			zeroes := 0
			for _, value := range values {
				maximum = max(maximum, value)
				if value == 0 {
					zeroes++
				}
			}
			if maximum < 2 {
				t.Fatalf("retries reset without new consumed progress: %v", values)
			}
			switch mode {
			case "silent":
				if zeroes != 0 {
					t.Fatalf("no reply counted as progress: %v", values)
				}
			case "challenge-only":
				if policy.challenges.Load() != 1 || zeroes != 1 {
					t.Fatalf("cached/duplicate challenge recounted as progress: challenges=%d counters=%v", policy.challenges.Load(), values)
				}
			case "no-evidence":
				if policy.type3.Load() < 2 {
					t.Fatal("did not progress through Type6 and Type2 into repeated Type3 timeouts")
				}
				seenRepeatedTimeout := false
				for _, value := range values {
					if value >= 2 {
						seenRepeatedTimeout = true
					}
					if seenRepeatedTimeout && value == 0 {
						t.Fatalf("duplicate metadata/notifications reset failures: %v", values)
					}
				}
			}
		})
	}
}

func TestExperimentalRecoveryCancellationWithDelayedProgress(t *testing.T) {
	policy := &delayedRecoveryPeer{}
	peer := newTestPeer(t, policy.responder("delayed"))
	durations := defaultTestDurations()
	durations.keepalive = 10 * time.Millisecond
	durations.heartbeat = 30 * time.Millisecond
	run := experimentalTestRun(t, peer, durations)
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	policy.online.Store(true)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for policy.challenges.Load() == 0 {
		select {
		case failure := <-done:
			t.Fatalf("unexpected exit: %#v", failure)
		case <-timer.C:
			t.Fatal("online request not sent")
		case <-ticker.C:
		}
	}
	cancel(context.Canceled)
	assertRunReturns(t, done, true, 300*time.Millisecond)
	if observer.establishedCallCount() != 1 {
		t.Fatal("cancelled recovery established a second run")
	}
}
