package d520

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

// udpReadBufferSize observes one byte beyond the 4096-byte accepted maximum,
// so oversized datagrams are rejected instead of accepted as truncated data.
const udpReadBufferSize = maxResponseDatagramLength + 1

// exchangeResponse classifies a received datagram within one exchange.
type exchangeResponse int

const (
	// responseAccept means the datagram is the expected response for the
	// current exchange and has been validated.
	responseAccept exchangeResponse = iota
	// responseIgnore means the datagram is a complete response provably
	// belonging to an already-sent KA exchange and may be discarded without
	// extending the exchange deadline or advancing state.
	responseIgnore
)

// responseClassifier validates one received datagram and classifies it as the
// expected response, an ignorable stale response, or invalid (non-nil error).
// It is phase-specific and supplied by the Run.
type responseClassifier func(datagram []byte) (exchangeResponse, error)

// udpExchange owns one connected udp4 socket for a single Run. The Run is the
// only user of the exchange, so all I/O is single-goroutine; only the
// cancellation callback runs concurrently and it touches only the socket
// deadline, never the read buffer.
type udpExchange struct {
	conn *net.UDPConn
	buf  [udpReadBufferSize]byte
}

// openUDPExchange dials one connected udp4 socket bound to the selected client
// IPv4 (with an OS-chosen port) and connected to the Profile endpoint. The
// returned exchange owns the socket and must be closed by the Run.
func openUDPExchange(clientIPv4 [4]byte, serverAddr netip.Addr, serverPort uint16) (*udpExchange, error) {
	localAddr := &net.UDPAddr{IP: append(net.IP(nil), clientIPv4[:]...), Port: 0}
	remoteAddr := net.UDPAddrFromAddrPort(netip.AddrPortFrom(serverAddr, serverPort))
	conn, err := net.DialUDP("udp4", localAddr, remoteAddr)
	if err != nil {
		return nil, err
	}
	return &udpExchange{conn: conn}, nil
}

func (ex *udpExchange) close() error {
	return ex.conn.Close()
}

// roundTrip performs one blocking request/response exchange. It sets one
// absolute socket deadline from the phase timeout before installing a
// short-lived cancellation callback; the callback advances the socket deadline
// to now to wake a blocking I/O when the exchange context is cancelled, and it
// never closes the socket. After I/O the callback is stopped, and if it had
// already started the exchange waits for it to finish before returning, so a
// later exchange can safely set a fresh deadline. The absolute deadline is
// never extended after an ignored datagram.
func (ex *udpExchange) roundTrip(
	ctx context.Context,
	timeout time.Duration,
	request []byte,
	classify responseClassifier,
	phase string,
	diagnostics protocol.AuthenticationProtocolDiagnostics,
) ([]byte, *runError) {
	deadline := time.Now().Add(timeout)
	if err := ex.conn.SetDeadline(deadline); err != nil {
		return nil, networkIOError("udp exchange", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	stop := context.AfterFunc(ctx, func() {
		defer wg.Done()
		_ = ex.conn.SetDeadline(time.Now())
	})
	defer func() {
		if !stop() {
			wg.Wait()
		}
	}()

	diagnostics.PhaseEvent(phase, phaseBoundaryBegin)
	diagnostics.DatagramEvent(phase, protocol.DatagramDirectionTx, request)
	if _, err := ex.conn.Write(request); err != nil {
		return nil, udpReadFailure(ctx, "udp exchange write", err)
	}

	for {
		n, err := ex.conn.Read(ex.buf[:])
		if err != nil {
			return nil, udpReadFailure(ctx, "udp exchange read", err)
		}
		datagram := make([]byte, n)
		copy(datagram, ex.buf[:n])
		diagnostics.DatagramEvent(phase, protocol.DatagramDirectionRx, datagram)
		class, classifyErr := classify(datagram)
		if classifyErr != nil {
			return nil, responseInvalidError("udp exchange response", classifyErr)
		}
		if class == responseAccept {
			diagnostics.PhaseEvent(phase, phaseBoundaryEnd)
			return datagram, nil
		}
		// responseIgnore: discard the stale datagram and keep reading against
		// the same absolute deadline without extending it.
	}
}

// udpReadFailure maps a write/read error to a network failure, distinguishing
// context cancellation (still a timeout-class failure here; the Run decides
// cancellation handling from the context cause) from a real timeout and other
// I/O errors.
func udpReadFailure(ctx context.Context, operation string, err error) *runError {
	if ctx.Err() != nil {
		return networkTimeoutError(operation, err)
	}
	if isTimeout(err) {
		return networkTimeoutError(operation, err)
	}
	return networkIOError(operation, err)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
