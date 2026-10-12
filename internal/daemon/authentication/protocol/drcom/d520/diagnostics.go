package d520

import (
	"errors"
	"fmt"
	protocol "sidravia/internal/daemon/authentication/protocol"
)

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
