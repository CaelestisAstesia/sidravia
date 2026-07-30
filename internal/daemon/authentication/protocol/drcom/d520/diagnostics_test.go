package d520

import (
	"context"
	"encoding/hex"
	"net"
	"net/netip"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

// TestDiagnosticPhaseConstantsAreStable proves the stable phase identifiers
// and boundary markers recorded by the diagnostic sink match the operator
// contract exactly.
func TestDiagnosticPhaseConstantsAreStable(t *testing.T) {
	want := []struct {
		name  string
		value string
	}{
		{phaseChallenge, "challenge"},
		{phaseLogin, "login"},
		{phaseBootstrapKA1, "bootstrap_ka1"},
		{phaseBootstrapKA2, "bootstrap_ka2"},
		{phaseKeepaliveKA1, "keepalive_ka1"},
		{phaseKeepaliveKA2, "keepalive_ka2"},
		{phaseLogout, "logout"},
	}
	for _, c := range want {
		if c.name != c.value {
			t.Errorf("phase constant %q != %q", c.name, c.value)
		}
	}
	if phaseBoundaryBegin != "begin" {
		t.Errorf("phaseBoundaryBegin = %q, want begin", phaseBoundaryBegin)
	}
	if phaseBoundaryEnd != "end" {
		t.Errorf("phaseBoundaryEnd = %q, want end", phaseBoundaryEnd)
	}
}

// captureDiagnostics records every diagnostic event in order for assertion. It
// never alters protocol behavior.
type captureDiagnostics struct {
	phases    []string
	datagrams []captureDatagram
}

type captureDatagram struct {
	phase     string
	direction protocol.AuthenticationProtocolDatagramDirection
	hex       string
}

func (d *captureDiagnostics) PhaseEvent(phase, boundary string) {
	d.phases = append(d.phases, phase+":"+boundary)
}

func (d *captureDiagnostics) DatagramEvent(phase string, direction protocol.AuthenticationProtocolDatagramDirection, datagram []byte) {
	d.datagrams = append(d.datagrams, captureDatagram{phase: phase, direction: direction, hex: hex.EncodeToString(datagram)})
}

// TestRoundTripRecordsTxRxAndPhaseBoundaries proves the UDP exchange records
// the TX datagram before the write, every RX datagram immediately after the
// read (including ignored ones), and phase begin/end boundaries, without
// altering the returned response.
func TestRoundTripRecordsTxRxAndPhaseBoundaries(t *testing.T) {
	serverConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer serverConn.Close()
	serverAddr := serverConn.LocalAddr().(*net.UDPAddr)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, client, err := serverConn.ReadFrom(buf)
			if err != nil {
				return
			}
			// Echo a valid-looking response so the classifier accepts it.
			response := append([]byte{0x99}, buf[:n]...)
			if _, err := serverConn.WriteTo(response, client); err != nil {
				return
			}
		}
	}()

	serverIP, ok := netip.AddrFromSlice(serverAddr.IP.To4())
	if !ok {
		t.Fatalf("cannot parse server IP %v", serverAddr.IP)
	}
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, serverIP, uint16(serverAddr.Port))
	if err != nil {
		t.Fatalf("openUDPExchange: %v", err)
	}
	defer ex.close()

	sink := &captureDiagnostics{}
	classify := func(datagram []byte) (exchangeResponse, error) {
		// Accept the echoed response marker.
		if len(datagram) > 0 && datagram[0] == 0x99 {
			return responseAccept, nil
		}
		return responseIgnore, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request := []byte{0x01, 0x02, 0x03}
	resp, failure := ex.roundTrip(ctx, 500*time.Millisecond, request, classify, phaseChallenge, sink)
	if failure != nil {
		t.Fatalf("roundTrip failure: %v", failure)
	}
	if len(resp) == 0 || resp[0] != 0x99 {
		t.Fatalf("roundTrip response = %v, want echoed response", resp)
	}

	if len(sink.datagrams) < 2 {
		t.Fatalf("expected at least TX+RX datagrams, got %d", len(sink.datagrams))
	}
	tx := sink.datagrams[0]
	if tx.direction != protocol.DatagramDirectionTx || tx.phase != phaseChallenge || tx.hex != "010203" {
		t.Errorf("TX datagram = %+v, want tx/challenge/010203", tx)
	}
	rx := sink.datagrams[1]
	if rx.direction != protocol.DatagramDirectionRx || rx.phase != phaseChallenge {
		t.Errorf("RX datagram = %+v, want rx/challenge", rx)
	}
	if rx.hex != "99010203" {
		t.Errorf("RX datagram hex = %q, want 99010203", rx.hex)
	}
	if len(sink.phases) < 2 || sink.phases[0] != "challenge:begin" || sink.phases[len(sink.phases)-1] != "challenge:end" {
		t.Errorf("phase boundaries = %v, want begin..end", sink.phases)
	}
}

func TestRoundTripEndsPhaseOnReadTimeout(t *testing.T) {
	serverConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer serverConn.Close()
	serverAddr := serverConn.LocalAddr().(*net.UDPAddr)
	serverIP, ok := netip.AddrFromSlice(serverAddr.IP.To4())
	if !ok {
		t.Fatalf("cannot parse server IP %v", serverAddr.IP)
	}
	ex, err := openUDPExchange([4]byte{127, 0, 0, 1}, localPort{mode: localPortSystemAssigned}, serverIP, uint16(serverAddr.Port))
	if err != nil {
		t.Fatalf("openUDPExchange: %v", err)
	}
	defer ex.close()

	sink := &captureDiagnostics{}
	_, failure := ex.roundTrip(
		context.Background(),
		10*time.Millisecond,
		[]byte{0x01},
		func([]byte) (exchangeResponse, error) { return responseAccept, nil },
		phaseChallenge,
		sink,
	)
	if failure == nil {
		t.Fatal("roundTrip expected timeout failure")
	}
	want := []string{"challenge:begin", "challenge:end"}
	if len(sink.phases) != len(want) || sink.phases[0] != want[0] || sink.phases[1] != want[1] {
		t.Errorf("phase boundaries = %v, want %v", sink.phases, want)
	}
}
