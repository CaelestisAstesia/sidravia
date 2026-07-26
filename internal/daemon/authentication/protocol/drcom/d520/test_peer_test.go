package d520

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

// testPeer is a minimal package-test UDP peer bound to 127.0.0.1:0. It is test
// code only; it is not the existing Python mock and does not claim campus
// protocol correctness. It records every request it receives and calls a
// configurable responder to produce zero or more response datagrams per
// request. All goroutines and sockets are explicitly cancelled, closed and
// awaited via t.Cleanup.
type testPeer struct {
	conn *net.UDPConn
	addr *net.UDPAddr

	mu       sync.Mutex
	received [][]byte
	respond  func([]byte) [][]byte

	wg           sync.WaitGroup
	stopped      atomic.Bool
	shutdownOnce sync.Once
}

func newTestPeer(t *testing.T, respond func([]byte) [][]byte) *testPeer {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	peer := &testPeer{
		conn:    conn,
		addr:    addr,
		respond: respond,
	}
	peer.wg.Add(1)
	go peer.serve()
	t.Cleanup(peer.shutdown)
	return peer
}

func (p *testPeer) serve() {
	defer p.wg.Done()
	defer p.stopped.Store(true)
	buf := make([]byte, 1500)
	for {
		n, clientAddr, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		request := make([]byte, n)
		copy(request, buf[:n])
		p.mu.Lock()
		p.received = append(p.received, request)
		respond := p.respond
		p.mu.Unlock()
		if respond == nil {
			continue
		}
		for _, response := range respond(request) {
			if _, err := p.conn.WriteToUDP(response, clientAddr); err != nil {
				return
			}
		}
	}
}

// shutdown closes the listener and waits for the serve goroutine to exit. It
// is idempotent.
func (p *testPeer) shutdown() {
	p.shutdownOnce.Do(func() {
		_ = p.conn.Close()
	})
	p.wg.Wait()
}

// setRespond swaps the responder. Used to change behavior mid-run.
func (p *testPeer) setRespond(respond func([]byte) [][]byte) {
	p.mu.Lock()
	p.respond = respond
	p.mu.Unlock()
}

func (p *testPeer) port() int { return p.addr.Port }

// requests returns a copy of every received request in arrival order.
func (p *testPeer) requests() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.received))
	copy(out, p.received)
	return out
}

// requestOpNames summarizes received requests by operation for assertions.
func (p *testPeer) requestOpNames() []string {
	requests := p.requests()
	out := make([]string, len(requests))
	for i, req := range requests {
		out[i] = peerRequestOp(req)
	}
	return out
}

func peerRequestOp(req []byte) string {
	switch {
	case isChallengeReq(req):
		return "challenge"
	case isLoginReq(req):
		return "login"
	case isKA1Req(req):
		return "ka1"
	case isKA2Req(req):
		return "ka2"
	case isLogoutReq(req):
		return "logout"
	default:
		return "unknown"
	}
}

func isChallengeReq(req []byte) bool { return len(req) >= 2 && req[0] == 0x01 && req[1] == 0x02 }
func isLoginReq(req []byte) bool     { return len(req) >= 2 && req[0] == 0x03 && req[1] == 0x01 }
func isKA1Req(req []byte) bool       { return len(req) >= 1 && req[0] == 0xff }
func isKA2Req(req []byte) bool       { return len(req) >= 6 && req[0] == 0x07 }
func isLogoutReq(req []byte) bool    { return len(req) >= 2 && req[0] == 0x06 && req[1] == 0x01 }

// peerChallengeResponse builds a 16-byte Challenge response carrying salt and
// the loopback source IPv4.
func peerChallengeResponse(salt [4]byte) []byte {
	resp := make([]byte, 16)
	resp[0] = 0x02
	copy(resp[4:8], salt[:])
	copy(resp[8:12], []byte{127, 0, 0, 1})
	return resp
}

func peerLoginSuccess(authInfo [16]byte) []byte {
	resp := make([]byte, 64)
	resp[0] = 0x04
	copy(resp[23:39], authInfo[:])
	return resp
}

func peerLoginRejection(code byte) []byte {
	resp := make([]byte, 32)
	resp[0] = 0x05
	resp[4] = code
	return resp
}

func peerKA1Response() []byte {
	resp := make([]byte, 20)
	resp[0] = 0x07
	return resp
}

// peerKA2Response builds a 60-byte KA2 response echoing serial and type and
// carrying tail. The tail is test-chosen; the Run stores whatever it receives.
func peerKA2Response(serial, typ byte, tail [4]byte) []byte {
	resp := make([]byte, 60)
	resp[0] = 0x07
	resp[1] = serial
	resp[2] = 0x28
	resp[3] = 0x00
	resp[4] = 0x0b
	resp[5] = typ
	copy(resp[16:20], tail[:])
	return resp
}

func peerLogoutACK() []byte { return []byte{0x04, 0x00, 0x00, 0x00} }

// fictionalExtensionFill is the deterministic filler for synthetic response
// extension bytes. Extensions are fictional padding; they contain no captured
// data and no executable signature. Only the required structural offsets of a
// synthetic response carry meaningful values.
const fictionalExtensionFill = 0xe5

// syntheticResponse returns a length-sized datagram filled with the fictional
// extension pattern; callers then set the required structural offsets.
func syntheticResponse(length int) []byte {
	resp := make([]byte, length)
	for i := range resp {
		resp[i] = fictionalExtensionFill
	}
	return resp
}

// peerChallengeResponseVariant builds a synthetic extended Challenge response
// carrying salt at [4,8) with fictional bytes everywhere else.
func peerChallengeResponseVariant(salt [4]byte, length int) []byte {
	resp := syntheticResponse(length)
	resp[0] = 0x02
	copy(resp[4:8], salt[:])
	return resp
}

// peerLoginSuccessVariant builds a synthetic extended Login success response
// carrying Auth Info at [23,39) with fictional bytes everywhere else.
func peerLoginSuccessVariant(authInfo [16]byte, length int) []byte {
	resp := syntheticResponse(length)
	resp[0] = 0x04
	copy(resp[23:39], authInfo[:])
	return resp
}

// peerKA1ResponseVariant builds a synthetic extended KA1 response: only the
// leading opcode byte is meaningful. The filler keeps bytes [2,5) away from
// the KA2 framing so the datagram cannot be mistaken for a KA2 response.
func peerKA1ResponseVariant(length int) []byte {
	resp := syntheticResponse(length)
	resp[0] = 0x07
	return resp
}

// peerKA2ResponseVariant builds a synthetic extended KA2 response echoing
// serial and carrying the given response type and Tail, with fictional bytes
// everywhere else.
func peerKA2ResponseVariant(serial, typ byte, tail [4]byte, length int) []byte {
	resp := syntheticResponse(length)
	resp[0] = 0x07
	resp[1] = serial
	resp[2] = 0x28
	resp[3] = 0x00
	resp[4] = 0x0b
	resp[5] = typ
	copy(resp[16:20], tail[:])
	return resp
}

// peerLogoutResponseVariant builds a synthetic extended Logout success
// response: only the leading opcode byte is meaningful.
func peerLogoutResponseVariant(length int) []byte {
	resp := syntheticResponse(length)
	resp[0] = 0x04
	return resp
}

// defaultPeerResponder answers the normal Challenge/Login/KA/Logout flow with
// a fixed salt and Auth Info and a tail derived from each KA2's serial/type.
func defaultPeerResponder() func([]byte) [][]byte {
	salt := [4]byte{0x01, 0x02, 0x03, 0x04}
	var authInfo [16]byte
	for i := range authInfo {
		authInfo[i] = byte(i + 1)
	}
	return func(req []byte) [][]byte {
		switch {
		case isChallengeReq(req):
			return [][]byte{peerChallengeResponse(salt)}
		case isLoginReq(req):
			return [][]byte{peerLoginSuccess(authInfo)}
		case isKA1Req(req):
			return [][]byte{peerKA1Response()}
		case isKA2Req(req):
			serial, typ := req[1], req[5]
			tail := [4]byte{serial, typ, 0xaa, 0xbb}
			return [][]byte{peerKA2Response(serial, typ, tail)}
		case isLogoutReq(req):
			return [][]byte{peerLogoutACK()}
		default:
			return nil
		}
	}
}

// recordingObserver records AuthenticationEstablished calls and, when a peer is
// attached, the number of requests the peer had received at the first call.
type recordingObserver struct {
	established           chan struct{}
	establishedCount      int
	establishedAtRequests int
	peer                  *testPeer
	mu                    sync.Mutex
}

func newRecordingObserver(peer *testPeer) *recordingObserver {
	return &recordingObserver{established: make(chan struct{}, 1), peer: peer}
}

func (o *recordingObserver) AuthenticationEstablished() {
	o.mu.Lock()
	o.establishedCount++
	count := 0
	if o.peer != nil {
		count = len(o.peer.requests())
	}
	if o.establishedCount == 1 {
		o.establishedAtRequests = count
	}
	o.mu.Unlock()
	select {
	case o.established <- struct{}{}:
	default:
	}
}

func (o *recordingObserver) establishedCallCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.establishedCount
}

// waitForEstablished waits up to timeout for the first AuthenticationEstablished.
func (o *recordingObserver) waitForEstablished(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-o.established:
	case <-time.After(timeout):
		t.Fatalf("authentication established was not observed within %s", timeout)
	}
}

var _ protocol.AuthenticationProtocolRunObserver = (*recordingObserver)(nil)
