package d520

import (
	"context"
	"net"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
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
				run.definition.cfg.localPort = localPort{mode: localPortFixed, value: port}
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
