package driver

import (
	"context"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"sidravia/internal/connection/auth/protocol"
)

func TestBusinessRejectsPopupMarkerWithValidChecksum(t *testing.T) {
	p, _ := hex.DecodeString("4d3884e323504f5055502353796e74686574696300")
	if _, ok := parseTextNotification(p, 0x1234); ok {
		t.Fatal("popup marker admitted as plain notification")
	}
}

func TestBusinessLoginFields(t *testing.T) {
	p := make([]byte, 39)
	p[0] = 4
	copy(p[9:17], []byte{1, 2, 3, 4, 0x39, 0x30, 0, 0})
	s, ok := parseLoginStatistics(p)
	if !ok || !s.HasTraffic || s.TrafficBytes != 67305985*1024 || !s.HasBalance || s.BalanceCents != 12345 {
		t.Fatalf("statistics %#v %v", s, ok)
	}
	for _, bytes := range [][]byte{{0, 0, 0, 0}, {255, 255, 255, 255}, {0, 0, 0, 128}} {
		copy(p[13:17], bytes)
		s, _ = parseLoginStatistics(p)
		if s.HasBalance != (bytes[3] == 0) {
			t.Fatal("unknown balance treated as value")
		}
	}
	for _, bad := range [][]byte{nil, p[:38], append([]byte{5}, p[1:]...)} {
		if _, ok := parseLoginStatistics(bad); ok {
			t.Fatal("invalid login statistics")
		}
	}
	p = make([]byte, 39)
	p[0] = 4
	s, _ = parseLoginStatistics(p)
	if !s.HasTraffic || !s.HasBalance || s.TrafficBytes != 0 || s.BalanceCents != 0 {
		t.Fatal("zero lost")
	}
}
func TestBusinessTextIndependentVectors(t *testing.T) {
	// Literal wire checksums calculated independently of the parser.
	for _, v := range []struct{ hex, text string }{{"4d381c2548656c6c6f00", "Hello"}, {"4d380cda4f4b00", "OK"}} {
		p, _ := hex.DecodeString(v.hex)
		text, ok := parseTextNotification(p, 0x1234)
		if !ok || text != v.text {
			t.Fatalf("%x %q %v", p, text, ok)
		}
		p[2] ^= 1
		if _, ok := parseTextNotification(p, 0x1234); ok {
			t.Fatal("checksum ignored")
		}
	}
	for _, p := range [][]byte{nil, {0x4d, 0x38, 0, 0}, {0x4d, 0x38, 0, 0, 'a', 'b'}, {0x4d, 0x38, 0, 0, 'a', 'b', 0, 1}, {0x4d, 0x38, 0, 0, 0xe4, 0xb8, 0}, {0x4d, 0x38, 0, 0, 'a', 27, 0}, {0x4d, 0x38, 0, 0, 'P', 'O', 'P', 'U', 'P', 0}, {0x4d, 0x38, 0, 0, 'D', 'R', 'C', 'O', 'M', 'W', 'E', 'B', 'M', 'S', 'G', 0}, {0x4d, 0x38, 0, 0, 'T', 'C', 'D', ' ', 0}} {
		if _, ok := parseTextNotification(p, 0); ok {
			t.Fatalf("invalid business accepted %x", p)
		}
	}
}

type businessTestObserver struct {
	*recordingObserver
	mu    sync.Mutex
	stats []protocol.AccountStatistics
	texts []string
}

func (o *businessTestObserver) ObserveAccountStatistics(s protocol.AccountStatistics) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stats = append(o.stats, s)
}
func (o *businessTestObserver) ObserveTextNotification(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.texts = append(o.texts, s)
}
func TestBusinessLoopbackDoesNotChangeAuthentication(t *testing.T) {
	base := experimentalResponder()
	peer := newTestPeer(t, func(p []byte) [][]byte {
		r := base(p)
		if isLoginReq(p) {
			copy(r[0][9:17], []byte{1, 0, 0, 0, 99, 0, 0, 0})
		}
		return r
	})
	run := experimentalTestRun(t, peer, defaultTestDurations())
	ctx, cancel := context.WithCancel(context.Background())
	o := &businessTestObserver{recordingObserver: newRecordingObserver(peer)}
	done := make(chan *protocol.AuthenticationProtocolRunFailure, 1)
	go func() { done <- run.Execute(ctx, o) }()
	o.waitForEstablished(t, time.Second)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		n := len(o.stats)
		o.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.stats) != 1 || o.stats[0].TrafficBytes != 1024 || o.stats[0].BalanceCents != 99 {
		t.Fatalf("stats %v", o.stats)
	}
}

func TestBusinessTokenIndependentVectors(t *testing.T) {
	exec := execution{definition: runDefinition{login: fictionalExperimentalInput()}, salt: [4]byte{0x11, 0x22, 0x33, 0x44}}
	for _, vector := range []struct{ flags, want uint16 }{{0, 56054}, {1, 64055}} {
		exec.experimental.loginFlags = vector.flags
		got, err := exec.messageToken()
		if err != nil || got != vector.want {
			t.Fatalf("token flags %d: %d %v", vector.flags, got, err)
		}
	}
}
