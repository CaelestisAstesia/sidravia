package driver

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"

	protocol "sidravia/internal/connection/auth/protocol"
)

func recordNetworkFailure(d protocol.AuthenticationProtocolDiagnostics, operation string, err error) {
	if sink, ok := d.(protocol.AuthenticationProtocolNetworkDiagnostics); ok {
		failure := protocol.AuthenticationProtocolNetworkFailure{Operation: operation, SystemCall: "unknown", TimedOut: isTimeout(err)}
		var code syscall.Errno
		if errors.As(err, &code) {
			failure.SystemCode = uint64(code)
		}
		var call *os.SyscallError
		if errors.As(err, &call) {
			switch call.Syscall {
			case "wsaioctl", "wsarecv", "wsarecvfrom", "wsasend", "wsasendto", "socket", "WSASocket", "WSASocketW", "setsockopt", "bind", "connect", "connectex", "ConnectEx", "getsockname", "getpeername", "WSARecv", "WSARecvFrom", "WSASend", "WSASendTo", "closesocket", "CreateIoCompletionPort":
				failure.SystemCall = call.Syscall
			}
		}
		var op *net.OpError
		if errors.As(err, &op) {
			if source, ok := op.Source.(*net.UDPAddr); ok {
				endpoint := source.AddrPort()
				failure.LocalEndpoint = netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())
			}
			if addr, ok := op.Addr.(*net.UDPAddr); ok {
				endpoint := addr.AddrPort()
				endpoint = netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())
				if operation == "set_deadline" {
					failure.LocalEndpoint = endpoint
				} else {
					failure.RemoteEndpoint = endpoint
				}
			}
		}
		sink.NetworkFailure(failure)
	}
}

type responseValidationError struct {
	outcome protocol.AuthenticationProtocolExchangeOutcome
	cause   error
}

func (e *responseValidationError) Error() string { return e.cause.Error() }
func (e *responseValidationError) Unwrap() error { return e.cause }

func invalidResponse(outcome protocol.AuthenticationProtocolExchangeOutcome, format string, args ...any) error {
	return &responseValidationError{outcome: outcome, cause: fmt.Errorf(format, args...)}
}

func responseOutcome(err error) protocol.AuthenticationProtocolExchangeOutcome {
	var invalid *responseValidationError
	if errors.As(err, &invalid) {
		return invalid.outcome
	}
	return protocol.ExchangeInvalidStructure
}

func recordExchange(d protocol.AuthenticationProtocolDiagnostics, phase string, outcome protocol.AuthenticationProtocolExchangeOutcome, length int) {
	if sink, ok := d.(protocol.AuthenticationProtocolExchangeDiagnostics); ok {
		sink.ExchangeOutcome(phase, outcome, length)
	}
}

// Stable diagnostic phase identifiers for D520 UDP exchanges. They are passed
// to the diagnostic sink and never derived from packet contents. The set
// matches the operator contract: challenge, login, bootstrap_ka1,
// bootstrap_ka2, keepalive_ka1, keepalive_ka2 and logout.
const (
	phaseChallenge    = "challenge"
	phaseLogin        = "login"
	phaseBootstrapKA1 = "bootstrap_ka1"
	phaseBootstrapKA2 = "bootstrap_ka2"
	phaseKeepaliveKA1 = "keepalive_ka1"
	phaseKeepaliveKA2 = "keepalive_ka2"
	phaseLogout       = "logout"
)

// Stable phase boundary markers recorded by the diagnostic sink at Debug level.
const (
	phaseBoundaryBegin = "begin"
	phaseBoundaryEnd   = "end"
)

func recordDecision(d protocol.AuthenticationProtocolDiagnostics, decision string, value int) {
	if sink, ok := d.(protocol.AuthenticationProtocolDecisionDiagnostics); ok {
		sink.DecisionEvent(decision, value)
	}
}
