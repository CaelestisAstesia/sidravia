package driver

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"sidravia/internal/connection/auth/d520/internal/settings"
	"sidravia/internal/connection/auth/protocol"
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
// IPv4 and the configured local port, and connected to the Profile endpoint.
// For localPortFixed the socket binds the configured fixed port; a bind conflict
// or permission failure is returned as an error and never falls back to a
// system-assigned port. For localPortSystemAssigned the local port is 0 and the
// operating system chooses a free port. The returned exchange owns the socket
// and must be closed by the Run.
func openUDPExchange(clientIPv4 [4]byte, port settings.LocalPort, serverAddr netip.Addr, serverPort uint16) (*udpExchange, error) {
	localPortNum := 0
	if port.Mode == settings.LocalPortFixed {
		localPortNum = int(port.Value)
	}
	localAddr := &net.UDPAddr{IP: append(net.IP(nil), clientIPv4[:]...), Port: localPortNum}
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
// never extended after an ignored datagram. A nil request receives until the
// scheduling deadline; reaching that deadline is a normal result, not a network failure.
func (ex *udpExchange) roundTrip(
	ctx context.Context,
	timeout time.Duration,
	request []byte,
	classify responseClassifier,
	phase string,
	diagnostics protocol.AuthenticationProtocolDiagnostics,
) ([]byte, *runError) {
	if ctx.Err() != nil {
		return nil, networkTimeoutError("udp exchange", ctx.Err())
	}
	deadline := time.Now().Add(timeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := ex.conn.SetDeadline(deadline); err != nil {
		recordNetworkFailure(diagnostics, "set_deadline", err)
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
	defer diagnostics.PhaseEvent(phase, phaseBoundaryEnd)
	if request != nil {
		diagnostics.DatagramEvent(phase, protocol.DatagramDirectionTx, request)
		if _, err := ex.conn.Write(request); err != nil {
			if ctx.Err() == nil {
				recordNetworkFailure(diagnostics, "write", err)
			}
			failure := udpIOFailure(ctx, "udp exchange write", err)
			recordExchange(diagnostics, phase, networkOutcome(failure), 0)
			return nil, failure
		}

	}
	for {
		if ctx.Err() != nil {
			return nil, networkTimeoutError("udp exchange deadline", ctx.Err())
		}
		if !time.Now().Before(deadline) {
			if request == nil {
				return nil, nil
			}
			return nil, networkTimeoutError("udp exchange deadline", nil)
		}
		n, err := ex.conn.Read(ex.buf[:])
		if err != nil {
			if request == nil && ctx.Err() == nil && isTimeout(err) {
				return nil, nil
			}
			if ctx.Err() == nil {
				recordNetworkFailure(diagnostics, "read", err)
			}
			failure := udpIOFailure(ctx, "udp exchange read", err)
			recordExchange(diagnostics, phase, networkOutcome(failure), 0)
			return nil, failure
		}
		datagram := make([]byte, n)
		copy(datagram, ex.buf[:n])
		diagnostics.DatagramEvent(phase, protocol.DatagramDirectionRx, datagram)
		class, classifyErr := classify(datagram)
		if classifyErr != nil {
			var runFailure *runError
			if errors.As(classifyErr, &runFailure) {
				recordExchange(diagnostics, phase, protocol.ExchangeControlReceived, n)
				return nil, runFailure
			}
			recordExchange(diagnostics, phase, responseOutcome(classifyErr), n)
			return nil, responseInvalidError("udp exchange response", classifyErr)
		}
		if class == responseAccept {
			recordExchange(diagnostics, phase, protocol.ExchangeAccepted, n)
			return datagram, nil
		}
		recordExchange(diagnostics, phase, protocol.ExchangeStaleIgnored, n)
		// responseIgnore: discard the stale datagram and keep reading against
		// the same absolute deadline without extending it.
	}
}

func networkOutcome(failure *runError) protocol.AuthenticationProtocolExchangeOutcome {
	if failure.kind == kindNetworkTimeout {
		return protocol.ExchangeNetworkTimeout
	}
	return protocol.ExchangeNetworkIOFailed
}

// udpIOFailure maps a write/read error to a network failure, distinguishing
// context cancellation (still a timeout-class failure here; the Run decides
// cancellation handling from the context cause) from a real timeout and other
// I/O errors.
func udpIOFailure(ctx context.Context, operation string, err error) *runError {
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
