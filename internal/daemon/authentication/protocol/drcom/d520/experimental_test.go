package d520

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

// Vectors use fictional credentials and documentation-only addresses. Expected
// values were independently calculated with Python hashlib/struct and OpenSSL
// MD4, without invoking these builders or importing packet captures.
func fictionalExperimentalInput() loginInput {
	return loginInput{jluExperimental: true, username: "alice", password: "example-password", mac: [6]byte{2, 1, 2, 3, 4, 5}, clientIPv4: [4]byte{192, 0, 2, 9}, hostName: "test-host", controlCheckStatus: 32, adapterNum: 1, ipdog: 1, authVersion: [2]byte{0x2c, 0}}
}

func TestExperimentalIndependentVectors(t *testing.T) {
	in := fictionalExperimentalInput()
	salt := [4]byte{0x11, 0x22, 0x33, 0x44}
	login, err := buildLoginRequest(in, salt)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(login)
	if hex.EncodeToString(sum[:]) != "d3611118a954e8ab899811c14fd8e6b331f016030762db77af61434b8c5bb4d6" {
		t.Fatalf("login hash %x", sum)
	}
	for _, part := range []struct {
		start, end int
		hex        string
	}{{97, 105, "4a9d861967320b5a"}, {314, 318, "50b56e1f"}, {328, 330, "3e3b"}} {
		if hex.EncodeToString(login[part.start:part.end]) != part.hex {
			t.Fatalf("login offset %d", part.start)
		}
	}
	logout, err := buildLogoutRequest(in, salt, [16]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(logout[4:20]) != "62fdd49c596acc11a809c6fcdb8af726" {
		t.Fatal("logout digest")
	}
	for i := 0; i < 6; i++ {
		if logout[58+i] != (logout[4+i] ^ in.mac[i]) {
			t.Fatal("logout MAC uses another digest")
		}
	}
	module := buildExperimentalModule(250, 3, 732, [4]byte{0x12, 0x34, 0x56, 0x78}, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, in.clientIPv4)
	if hex.EncodeToString(module) != "07fa28000b03dc0212345678000000000102030405060708f2d7fa01c00002090000000000000000" {
		t.Fatalf("module %x", module)
	}
	vectors := [][2]string{{"c72f31017e000000", "c72f31017e000000"}, {"4163e8a36a18b063", "3c03e7888d9a0dbe"}, {"005874c180ee2bb5", "8a6aa6713b488b2e"}, {"3b62246d1ce21116", "74a2490130c26e82"}}
	for mode, pair := range vectors {
		for ext, want := range pair {
			got := challengeDigest([4]byte{byte(mode), 0x22, 0x33, 0x44}, ext == 1)
			if hex.EncodeToString(got[:]) != want {
				t.Fatalf("mode %d extended %d: %x", mode, ext, got)
			}
		}
	}
	in.password = strings.Repeat("p", 33)
	if _, err := buildLoginRequest(in, salt); err == nil {
		t.Fatal("long password silently accepted")
	}
	in.jluExperimental = false
	if _, err := buildLoginRequest(in, salt); err != nil {
		t.Fatal("legacy password behavior changed", err)
	}
}

func experimentalTestRun(t *testing.T, peer *testPeer, d testDurations) *d520Run {
	t.Helper()
	raw := testConfigJSON(peer.port(), d)
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["behavior"] = "jlu-experimental"
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewFactory().CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs{InstitutionProtocolConfiguration: raw, AuthenticationCredential: testCredential(), SelectedSystemNetworkBinding: testBinding(t), SystemHostInformation: testHostInformation()})
	if err != nil {
		t.Fatal(err)
	}
	return run.(*d520Run)
}

func experimentalResponder() func([]byte) [][]byte {
	return func(req []byte) [][]byte {
		switch {
		case len(req) == 20 && req[0] == 1:
			p := make([]byte, 44)
			p[0] = 2
			copy(p[1:4], req[1:4])
			copy(p[4:8], []byte{0x11, 0x22, 0x33, 0x44})
			return [][]byte{p}
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess([16]byte{1, 2, 3, 4})}
		case isKA1Req(req):
			p := make([]byte, 20)
			p[0] = 7
			p[2] = 20
			p[4] = 6
			return [][]byte{p}
		case len(req) == 40 && req[0] == 7:
			p := append([]byte(nil), req...)
			p[5]++
			copy(p[16:24], []byte{1, 2, 3, 4, 5, 6, 7, 8})
			return [][]byte{p}
		case isLogoutReq(req):
			return [][]byte{peerLogoutACK()}
		}
		return nil
	}
}

func TestExperimentalPasswordExtensionIndicatorAllowsExistingLogin(t *testing.T) {
	for _, tc := range []struct {
		name      string
		indicator uint16
	}{
		{"zero", 0},
		{"enabled", 1},
		{"high_byte", 0x0100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := experimentalResponder()
			peer := newTestPeer(t, func(req []byte) [][]byte {
				responses := base(req)
				if len(req) == 20 && req[0] == 1 {
					// Synthetic extended challenge; no campus packet or credential data.
					challenge := make([]byte, 76)
					copy(challenge, responses[0])
					binary.LittleEndian.PutUint16(challenge[8:10], tc.indicator)
					return [][]byte{challenge}
				}
				return responses
			})
			run := experimentalTestRun(t, peer, defaultTestDurations())
			observer := newRecordingObserver(peer)
			_, cancel, done := startRun(t, run, observer)
			defer cancel(context.Canceled)
			observer.waitForEstablished(t, time.Second)
			cancel(protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: protocol.TerminateWithBestEffortLogout})
			assertRunReturns(t, done, true, time.Second)
			logins, logouts := 0, 0
			for _, req := range peer.requests() {
				if isLoginReq(req) {
					logins++
					if len(req) != 330 || !bytes.Equal(req[312:314], []byte{2, 12}) {
						t.Fatal("password extension indicator changed the existing login layout")
					}
				}
				if isLogoutReq(req) {
					logouts++
				}
			}
			if logins != 1 || logouts != 1 || observer.establishedCallCount() != 1 {
				t.Fatalf("logins=%d logouts=%d establishments=%d", logins, logouts, observer.establishedCallCount())
			}
		})
	}
}

func TestExperimentalProfilePolicy(t *testing.T) {
	var cfg map[string]any
	if err := json.Unmarshal(testConfigJSON(1234, defaultTestDurations()), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []any{"", nil, true, "future"} {
		cfg["behavior"] = mode
		raw, _ := json.Marshal(cfg)
		if _, err := decodeInstitutionProtocolConfiguration(raw); err == nil {
			t.Fatalf("mode accepted: %v", mode)
		}
	}
	cfg["behavior"] = "jlu-experimental"
	cfg["heartbeatInterval"] = "61s"
	raw, _ := json.Marshal(cfg)
	if _, err := decodeInstitutionProtocolConfiguration(raw); err == nil {
		t.Fatal("unbounded interval")
	}
}

func TestExperimentalModuleExchangeAndMetadata(t *testing.T) {
	for _, counter := range []int{0, 10} {
		for _, version := range []uint16{0, 732} {
			t.Run(strings.Join([]string{time.Duration(counter).String(), time.Duration(version).String()}, "/"), func(t *testing.T) {
				responder := experimentalResponder()
				first := true
				peer := newTestPeer(t, func(req []byte) [][]byte {
					if first {
						first = false
						p := append([]byte(nil), req...)
						p[5] = 6
						binary.LittleEndian.PutUint16(p[6:8], 733)
						binary.LittleEndian.PutUint32(p[16:20], 128)
						return [][]byte{p}
					}
					return responder(req)
				})
				run := experimentalTestRun(t, peer, defaultTestDurations())
				exec := newExecution(run.definition, newRecordingObserver(peer), protocol.NoopAuthenticationProtocolDiagnostics{})
				ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
				if err != nil {
					t.Fatal(err)
				}
				defer ex.close()
				exec.exchange = ex
				exec.experimental.moduleVersion = version
				exec.experimental.completions = counter
				exec.serial = 255
				if f := exec.sendKA2(context.Background(), 1, true, phaseBootstrapKA2); f != nil {
					t.Fatal(f)
				}
				if exec.serial != 1 || exec.experimental.moduleVersion != 733 || exec.experimental.challenge != [8]byte{1, 2, 3, 4, 5, 6, 7, 8} {
					t.Fatal("metadata/challenge/serial transition")
				}
				if f := exec.sendKA2(context.Background(), 3, false, phaseKeepaliveKA2); f != nil {
					t.Fatal(f)
				}
				requests := peer.requests()
				want := version
				if want == 0 {
					want = 9999
				}
				if counter == 10 {
					want = version - 1
				}
				if binary.LittleEndian.Uint16(requests[0][6:8]) != want || binary.LittleEndian.Uint16(requests[1][6:8]) != 733 {
					t.Fatal("version transition")
				}
				if binary.LittleEndian.Uint32(requests[1][12:16]) != 40 || binary.LittleEndian.Uint32(requests[2][12:16]) != 40 {
					t.Fatal("metadata offset lost")
				}
				if !bytes.Equal(requests[2][16:24], []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
					t.Fatal("lost challenge bytes")
				}
				if exec.experimental.completions != 1 && counter == 10 {
					t.Fatal("completion counter")
				}
			})
		}
	}
}

func TestExperimentalInterleavedAndLatePackets(t *testing.T) {
	base := experimentalResponder()
	var oldModule []byte
	peer := newTestPeer(t, func(req []byte) [][]byte {
		responses := base(req)
		if len(responses) == 0 {
			return nil
		}
		prefix := [][]byte{{7, 0, 6, 0, 99, 0}, {0x4d, 0x38, 0, 0}, {0x04}} // unknown 07, notification, old login ACK
		if len(req) == 40 && req[0] == 7 {
			if oldModule != nil {
				prefix = append(prefix, oldModule)
			}
			wrong := append([]byte(nil), responses[0]...)
			wrong[8] ^= 1
			prefix = append(prefix, wrong)
			oldModule = append([]byte(nil), responses[0]...)
		}
		// Do not inject an uncorrelated success into a pending login/logout.
		if isLoginReq(req) || isLogoutReq(req) {
			prefix = prefix[:2]
		}
		return append(prefix, responses...)
	})
	run := experimentalTestRun(t, peer, defaultTestDurations())
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	time.Sleep(100 * time.Millisecond)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	if observer.establishedCallCount() != 1 {
		t.Fatal("duplicate establishment")
	}
}

func TestExperimentalIdleControlAndCancellation(t *testing.T) {
	for _, control := range []byte{0x26, 0x15, 0x38} {
		t.Run(string(rune(control)), func(t *testing.T) {
			peer := newTestPeer(t, experimentalResponder())
			d := defaultTestDurations()
			d.heartbeat = time.Second
			run := experimentalTestRun(t, peer, d)
			observer := &socketRecordingObserver{recordingObserver: newRecordingObserver(peer)}
			_, cancel, done := startRun(t, run, observer)
			defer cancel(context.Canceled)
			observer.waitForEstablished(t, time.Second)
			packet := []byte{0x4d, control, 0, 0}
			if control == 0x15 {
				packet = append(packet, []byte("AdminReset\x00")...)
			}
			for i := 0; i < 2; i++ {
				if _, err := peer.conn.WriteToUDP(packet, net.UDPAddrFromAddrPort(observer.opened[0].local)); err != nil {
					t.Fatal(err)
				}
			}
			if control == 0x15 {
				failure := assertRunReturns(t, done, false, time.Second)
				if failure.Code != "server_session_reset" || failure.HandlingRecommendation != protocol.RetryAfterBoundedRecovery {
					t.Fatalf("%#v", failure)
				}
				for _, req := range peer.requests() {
					if isLogoutReq(req) {
						t.Fatal("logout after server reset")
					}
				}
			} else {
				if control == 0x26 {
					deadline := time.Now().Add(300 * time.Millisecond)
					for {
						count := 0
						for _, req := range peer.requests() {
							if req[0] == 0xfe {
								count++
							}
						}
						if count > 0 {
							if count != 1 {
								t.Fatal("confirmation not rate limited")
							}
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("idle control not received")
						}
						time.Sleep(time.Millisecond)
					}
				}
				start := time.Now()
				cancel(context.Canceled)
				assertRunReturns(t, done, true, time.Second)
				if time.Since(start) > 200*time.Millisecond {
					t.Fatal("cancellation waited for interval")
				}
			}
			assertSocketPortReleased(t, observer.opened[0].local)
		})
	}
}

func TestExperimentalLossRecoversWithoutAnotherLogin(t *testing.T) {
	base := experimentalResponder()
	var pings atomic.Int32
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if isKA1Req(req) && pings.Add(1) == 2 {
			return nil
		}
		return base(req)
	})
	d := defaultTestDurations()
	d.keepalive = 15 * time.Millisecond
	d.heartbeat = 30 * time.Millisecond
	run := experimentalTestRun(t, peer, d)
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	deadline := time.Now().Add(time.Second)
	for pings.Load() < 4 {
		if time.Now().After(deadline) {
			t.Fatal("recovery did not send later ping")
		}
		time.Sleep(time.Millisecond)
	}
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	logins := 0
	for _, req := range peer.requests() {
		if isLoginReq(req) {
			logins++
		}
	}
	if logins != 1 || observer.establishedCallCount() != 1 {
		t.Fatal("loss caused new authentication")
	}
}

func TestExperimentalIrrelevantTrafficCannotExtendDeadline(t *testing.T) {
	base := experimentalResponder()
	var pings atomic.Int32
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if isKA1Req(req) && pings.Add(1) > 1 {
			return [][]byte{{7, 0, 6, 0, 99, 0}, {0x4d, 0x38, 0, 0}}
		}
		return base(req)
	})
	d := defaultTestDurations()
	d.keepalive = 10 * time.Millisecond
	d.heartbeat = 20 * time.Millisecond
	d.logout = 10 * time.Millisecond
	run := experimentalTestRun(t, peer, d)
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	failure := assertRunReturns(t, done, false, 700*time.Millisecond)
	if failure.Code != "network_timeout" || pings.Load() < 2 {
		t.Fatalf("expiry %#v, pings %d", failure, pings.Load())
	}
}

func TestExperimentalWrongChallengeGenerationCannotEstablish(t *testing.T) {
	base := experimentalResponder()
	var stale []byte
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if len(req) == 20 && req[0] == 1 {
			reply := base(req)[0]
			if stale == nil {
				stale = reply
				return nil
			}
			return [][]byte{stale}
		}
		return base(req)
	})
	d := defaultTestDurations()
	d.challenge = 20 * time.Millisecond
	run := experimentalTestRun(t, peer, d)
	for i := 0; i < 2; i++ {
		observer := newRecordingObserver(peer)
		failure := run.Execute(context.Background(), observer)
		if failure == nil || failure.Code != "network_timeout" || observer.establishedCallCount() != 0 {
			t.Fatal("stale generation established")
		}
	}
}

func TestExperimentalLogoutUsesFreshChallengeAndTotalBudget(t *testing.T) {
	base := experimentalResponder()
	peer := newTestPeer(t, base)
	d := defaultTestDurations()
	d.logout = 25 * time.Millisecond
	run := experimentalTestRun(t, peer, d)
	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	observer.waitForEstablished(t, time.Second)
	peer.setRespond(func(req []byte) [][]byte {
		if len(req) == 20 && req[0] == 1 {
			return nil
		}
		return base(req)
	})
	start := time.Now()
	cancel(protocol.AuthenticationProtocolRunCancellationCause{CleanupRequirement: protocol.TerminateWithBestEffortLogout})
	assertRunReturns(t, done, true, 300*time.Millisecond)
	if time.Since(start) > 150*time.Millisecond {
		t.Fatal("logout exceeded total budget")
	}
	requests := peer.requests()
	last := requests[len(requests)-1]
	if !isLogoutReq(last) {
		t.Fatal("no fallback logout")
	}
	want, _ := buildLogoutRequest(run.definition.login, [4]byte{0x11, 0x22, 0x33, 0x44}, [16]byte{1, 2, 3, 4})
	if !bytes.Equal(last, want) {
		t.Fatal("logout digest or saved salt mismatch")
	}
}

func TestExperimentalOnlineConfirmationVectors(t *testing.T) {
	for flag, want := range []string{"fed0018f11bfd6fad6d4af5d93a6d293e27662ff000102030405060708090a0b0c0d0e0f", "fe310177230268df7175d8a8f9a29f8f7a3907ff000102030405060708090a0b0c0d0e0f"} {
		peer := newTestPeer(t, nil)
		run := experimentalTestRun(t, peer, defaultTestDurations())
		exec := newExecution(run.definition, newRecordingObserver(peer), protocol.NoopAuthenticationProtocolDiagnostics{})
		exec.definition.login = fictionalExperimentalInput()
		exec.salt = [4]byte{0x11, 0x22, 0x33, 0x44}
		exec.hasAuthInfo = true
		exec.experimental.loginFlags = uint16(flag)
		for i := range exec.authInfo {
			exec.authInfo[i] = byte(i)
		}
		ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
		if err != nil {
			t.Fatal(err)
		}
		exec.exchange = ex
		if f := exec.handleControl(context.Background(), []byte{0x4d, 0x26, 0, 0}); f != nil {
			t.Fatal(f)
		}
		deadline := time.Now().Add(time.Second)
		for len(peer.requests()) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("no confirmation")
			}
			time.Sleep(time.Millisecond)
		}
		if hex.EncodeToString(peer.requests()[0]) != want {
			t.Fatalf("flag %d reply %x", flag, peer.requests()[0])
		}
		if err := ex.close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExperimentalChallengePingNegotiation(t *testing.T) {
	for mode := byte(0); mode < 4; mode++ {
		for _, extended := range []bool{false, true} {
			t.Run(hex.EncodeToString([]byte{mode, byte(map[bool]int{false: 0, true: 1}[extended])}), func(t *testing.T) {
				base := experimentalResponder()
				var challengeRequests atomic.Int32
				peer := newTestPeer(t, func(req []byte) [][]byte {
					if len(req) == 20 && req[0] == 1 {
						responses := base(req)
						responses[0][43] = 0x80
						return responses
					}
					if len(req) == 8 && req[0] == 7 {
						challengeRequests.Add(1)
						p := make([]byte, 16)
						p[0] = 7
						p[1] = req[1]
						p[2] = 16
						p[4] = 2
						if extended {
							p[5] = 0x80
						}
						copy(p[8:12], []byte{mode, 0x22, 0x33, 0x44})
						return [][]byte{p}
					}
					return base(req)
				})
				run := experimentalTestRun(t, peer, defaultTestDurations())
				observer := newRecordingObserver(peer)
				_, cancel, done := startRun(t, run, observer)
				defer cancel(context.Canceled)
				observer.waitForEstablished(t, time.Second)
				cancel(context.Canceled)
				assertRunReturns(t, done, true, time.Second)
				if challengeRequests.Load() != 1 {
					t.Fatal("negotiation did not select challenge ping")
				}
				for _, req := range peer.requests() {
					if isKA1Req(req) {
						if mode != 0 {
							want := challengeDigest([4]byte{mode, 0x22, 0x33, 0x44}, extended)
							if !bytes.Equal(req[12:20], want[:]) {
								t.Fatal("challenge digest placement")
							}
						}
					}
				}
			})
		}
	}
}

func TestExperimentalModuleRejectsInvalidFramesAndRepeatedMetadata(t *testing.T) {
	for _, variant := range []string{"length", "type", "metadata-size", "metadata-repeat", "zero-version"} {
		t.Run(variant, func(t *testing.T) {
			base := experimentalResponder()
			peer := newTestPeer(t, func(req []byte) [][]byte {
				if len(req) == 40 && req[0] == 7 {
					p := base(req)[0]
					switch variant {
					case "length":
						p[2]++
					case "type":
						p[5] = 4
					case "metadata-size":
						p[5] = 6
						binary.LittleEndian.PutUint32(p[16:20], 262145)
					case "metadata-repeat":
						p[5] = 6
						binary.LittleEndian.PutUint32(p[16:20], 128)
					case "zero-version":
						p[6] = 0
						p[7] = 0
					}
					return [][]byte{p}
				}
				return base(req)
			})
			run := experimentalTestRun(t, peer, defaultTestDurations())
			observer := newRecordingObserver(peer)
			failure := run.Execute(context.Background(), observer)
			want := protocol.AuthenticationProtocolFailureCode("protocol_response_invalid")
			if variant == "zero-version" {
				want = "negotiation_unsupported"
			}
			if failure == nil || failure.Code != want || observer.establishedCallCount() != 0 {
				t.Fatalf("failure %#v", failure)
			}
		})
	}
}

func TestExperimentalModuleSerialWrapRejectsOldRandomEcho(t *testing.T) {
	base := experimentalResponder()
	var old []byte
	peer := newTestPeer(t, func(req []byte) [][]byte {
		p := base(req)[0]
		if old == nil {
			old = append([]byte(nil), p...)
			return [][]byte{p}
		}
		return [][]byte{old, p}
	})
	run := experimentalTestRun(t, peer, defaultTestDurations())
	exec := newExecution(run.definition, newRecordingObserver(peer), protocol.NoopAuthenticationProtocolDiagnostics{})
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.close()
	exec.exchange = ex
	for i := 0; i < 258; i++ {
		if f := exec.sendKA2(context.Background(), 1, false, phaseKeepaliveKA2); f != nil {
			t.Fatalf("exchange %d: %v", i, f)
		}
	}
	if exec.serial != 2 || len(peer.requests()) != 258 {
		t.Fatal("serial wrap advancement")
	}
}

func TestExperimentalCancelDuringProtocolWaits(t *testing.T) {
	for _, phase := range []string{"challenge", "login", "ka1", "ka2", "busy-backoff"} {
		t.Run(phase, func(t *testing.T) {
			reached := make(chan struct{}, 1)
			base := experimentalResponder()
			peer := newTestPeer(t, func(req []byte) [][]byte {
				op := peerRequestOp(req)
				if len(req) == 20 && req[0] == 1 {
					op = "challenge"
				}
				if op == phase || (phase == "busy-backoff" && op == "login") {
					select {
					case reached <- struct{}{}:
					default:
					}
					if phase == "busy-backoff" {
						return [][]byte{peerLoginRejection(2)}
					}
					return nil
				}
				return base(req)
			})
			run := experimentalTestRun(t, peer, defaultTestDurations())
			observer := newRecordingObserver(peer)
			_, cancel, done := startRun(t, run, observer)
			defer cancel(context.Canceled)
			select {
			case <-reached:
			case <-time.After(time.Second):
				t.Fatal("phase not reached")
			}
			start := time.Now()
			cancel(context.Canceled)
			assertRunReturns(t, done, true, 300*time.Millisecond)
			if time.Since(start) > 150*time.Millisecond {
				t.Fatal("cancellation waited for exchange deadline")
			}
			if observer.establishedCallCount() != 0 {
				t.Fatal("cancelled handshake established")
			}
		})
	}
}

func TestExperimentalServerOfflineHasRetryAdviceAndNoLogout(t *testing.T) {
	base := experimentalResponder()
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if len(req) == 40 && req[0] == 7 && req[5] == 3 {
			return [][]byte{{0x4d, 0x15, 0, 0}}
		}
		return base(req)
	})
	run := experimentalTestRun(t, peer, defaultTestDurations())
	failure := run.Execute(context.Background(), newRecordingObserver(peer))
	if failure == nil || failure.Code != "server_session_ended" || failure.HandlingRecommendation != protocol.RetryAfterStandardDelay {
		t.Fatalf("%#v", failure)
	}
	for _, req := range peer.requests() {
		if isLogoutReq(req) {
			t.Fatal("logout after explicit offline")
		}
	}
}

func TestExperimentalNegotiatedIntervalBounds(t *testing.T) {
	exec := &execution{diagnostics: protocol.NoopAuthenticationProtocolDiagnostics{}}
	for _, seconds := range []byte{0, 15, 16, 60, 61, 255} {
		exec.experimental.interval = 20 * time.Second
		exec.learnInterval([]byte{7, 0, 6, 0, 6, seconds})
		want := 20 * time.Second
		if seconds >= 16 && seconds <= 60 {
			want = time.Duration(seconds) * time.Second
		}
		if exec.experimental.interval != want {
			t.Fatalf("interval %d", seconds)
		}
	}
}

func TestExperimentalChallengePingFullVectors(t *testing.T) {
	vectors := [][2]string{
		{"ff37faab3ed558fdfae30547260c554545000000000102030405060708090a0b0c0d0e0f341200000000", "ff37faab3ed558fdfae30547260c554545000000000102030405060708090a0b0c0d0e0f341200000000"},
		{"ff00000000000000012233444163e8a36a18b063000102030405060708090a0b0c0d0e0f341200000000", "ff00000000000000012233443c03e7888d9a0dbe000102030405060708090a0b0c0d0e0f341200000000"},
		{"ff0000000000000002223344005874c180ee2bb5000102030405060708090a0b0c0d0e0f341200000000", "ff00000000000000022233448a6aa6713b488b2e000102030405060708090a0b0c0d0e0f341200000000"},
		{"ff00000000000000032233443b62246d1ce21116000102030405060708090a0b0c0d0e0f341200000000", "ff000000000000000322334474a2490130c26e82000102030405060708090a0b0c0d0e0f341200000000"},
	}
	var auth [16]byte
	for i := range auth {
		auth[i] = byte(i)
	}
	digest, _ := hex.DecodeString("37faab3ed558fdfae30547260c554545")
	for mode, pair := range vectors {
		for ext, want := range pair {
			packet, err := buildChallengePingRequest(digest, auth, [4]byte{byte(mode), 0x22, 0x33, 0x44}, ext == 1, 0x1234)
			if err != nil || hex.EncodeToString(packet) != want {
				t.Fatalf("mode=%d extended=%d packet=%x err=%v", mode, ext, packet, err)
			}
		}
	}
}

func TestExperimentalIdleDeadlineIsNotNetworkFailure(t *testing.T) {
	peer := newTestPeer(t, nil)
	run := experimentalTestRun(t, peer, defaultTestDurations())
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.close()
	for _, phase := range []string{"online_receive", "login_backoff"} {
		diagnostics := &captureDiagnostics{}
		response, failure := ex.roundTrip(context.Background(), 10*time.Millisecond, nil, func([]byte) (exchangeResponse, error) { return responseIgnore, nil }, phase, diagnostics)
		if failure != nil || response != nil || len(diagnostics.networkFailures) != 0 || len(diagnostics.outcomes) != 0 {
			t.Fatalf("normal timer expiration diagnosed as failure: %#v %#v", failure, diagnostics)
		}
	}
	diagnostics := &captureDiagnostics{}
	_, failure := ex.roundTrip(context.Background(), 10*time.Millisecond, []byte{1}, func([]byte) (exchangeResponse, error) { return responseIgnore, nil }, phaseChallenge, diagnostics)
	if failure == nil || failure.kind != kindNetworkTimeout || len(diagnostics.networkFailures) != 1 {
		t.Fatal("request timeout no longer reported")
	}
}

func TestExperimentalChallengePingRejectsOtherGenerationSequence(t *testing.T) {
	base := experimentalResponder()
	peer := newTestPeer(t, func(req []byte) [][]byte {
		if len(req) == 8 && req[0] == 7 {
			p := make([]byte, 16)
			p[0] = 7
			p[1] = req[1] - 1
			p[2] = 16
			p[4] = 2
			return [][]byte{p}
		}
		return base(req)
	})
	run := experimentalTestRun(t, peer, defaultTestDurations())
	exec := newExecution(run.definition, newRecordingObserver(peer), protocol.NoopAuthenticationProtocolDiagnostics{})
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, run.definition.cfg.serverAddress, uint16(peer.port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.close()
	exec.exchange = ex
	if f := exec.challenge(context.Background()); f != nil {
		t.Fatal(f)
	}
	requests := peer.requests()
	if exec.experimental.pingSerial != requests[0][1]+1 {
		t.Fatal("ping sequence lost challenge correlation")
	}
	if f := exec.sendChallengePing(context.Background(), phaseBootstrapKA1); f == nil || f.kind != kindNetworkTimeout {
		t.Fatal("other sequence accepted")
	}
	for _, request := range peer.requests() {
		if isKA1Req(request) {
			t.Fatal("stale challenge advanced ping")
		}
	}
}

func TestExperimentalPingTimestampLowWord(t *testing.T) {
	for _, vector := range []struct {
		seconds  int64
		expected [2]byte
	}{
		{0x1234, [2]byte{0x34, 0x12}}, {65535, [2]byte{0xff, 0xff}}, {65536, [2]byte{0, 0}}, {65537, [2]byte{1, 0}},
	} {
		packet, err := buildExperimentalPingRequest(make([]byte, 16), [16]byte{}, vector.seconds)
		if err != nil || !bytes.Equal(packet[36:38], vector.expected[:]) {
			t.Fatalf("timestamp %d: %x %v", vector.seconds, packet, err)
		}
	}
}
