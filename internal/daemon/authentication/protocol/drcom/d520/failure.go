package d520

import (
	"fmt"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

// runErrorKind classifies a D520 execution failure for mapping to a stable
// public AuthenticationProtocolRunFailure.
type runErrorKind uint8

const (
	kindNetworkTimeout runErrorKind = iota + 1
	kindNetworkIO
	kindServerBusyExhausted
	kindKnownRejection
	kindUnknownRejection
	kindResponseInvalid
	kindContractViolated
	kindCleanupFailed
)

// runError is the private typed execution failure carried inside d520. It
// preserves the operation, a failure kind, an optional wire rejection code and
// a wrapped cause. It is never exposed outside the package; only its mapped,
// static public projection leaves d520.
type runError struct {
	operation   string
	kind        runErrorKind
	wireCode    byte
	hasWireCode bool
	cause       error
}

func (e *runError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = ": " + e.cause.Error()
	}
	if e.hasWireCode {
		return fmt.Sprintf("d520 %s failed (wire code 0x%02x)%s", e.operation, e.wireCode, cause)
	}
	return fmt.Sprintf("d520 %s failed%s", e.operation, cause)
}

func (e *runError) Unwrap() error { return e.cause }

// knownRejectionTable maps the Login rejection wire codes from protocol
// specification section 9 to stable public codes and static descriptions. The
// descriptions never reveal whether an account exists.
var knownRejectionTable = map[byte]struct {
	code        string
	description string
}{
	0x01: {"session_in_use", "The account is already in use on another endpoint."},
	0x02: {"server_busy", "The authentication server reported busy."},
	0x03: {"credential_invalid", "The credential was rejected by the server."},
	0x04: {"insufficient_funds", "The account has insufficient funds."},
	0x05: {"account_frozen", "The account is frozen."},
	0x07: {"binding_ip_mismatch", "The reported IPv4 address was rejected by the server."},
	0x0B: {"binding_mac_mismatch", "The reported MAC address was rejected by the server."},
	0x14: {"too_many_sessions", "The account has exceeded its session limit."},
	0x15: {"incompatible_version", "The reported protocol version is incompatible with the server."},
	0x16: {"binding_pair_mismatch", "The reported IPv4 and MAC pair was rejected by the server."},
	0x17: {"dhcp_required", "The server requires a DHCP address but none was reported."},
}

// toFailure maps a runError to a static, safe public
// AuthenticationProtocolRunFailure. The DiagnosticCause preserves the operation
// and wrapped causes but never carries credentials, raw JSON, packet hex or
// digest material.
func (e *runError) toFailure() *protocol.AuthenticationProtocolRunFailure {
	var code, description string
	var recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation
	switch e.kind {
	case kindNetworkTimeout:
		code, description, recommendation = "network_timeout", "Network operation timed out.", protocol.RetryAfterStandardDelay
	case kindNetworkIO:
		code, description, recommendation = "network_io_failed", "Network I/O failed.", protocol.RetryAfterStandardDelay
	case kindServerBusyExhausted:
		code, description, recommendation = "server_busy", "The authentication server reported busy and the bounded retry was exhausted.", protocol.RetryAfterExtendedDelay
	case kindKnownRejection:
		entry := knownRejectionTable[e.wireCode]
		code, description, recommendation = entry.code, entry.description, protocol.BlockUntilExplicitRestartOrRelevantInputChange
	case kindUnknownRejection:
		code, description, recommendation = "authentication_rejected", "Authentication was rejected by the server.", protocol.BlockUntilExplicitRestartOrRelevantInputChange
	case kindResponseInvalid:
		code, description, recommendation = "protocol_response_invalid", "The server response was malformed or incompatible.", protocol.BlockUntilExplicitRestartOrRelevantInputChange
	case kindContractViolated:
		code, description, recommendation = "protocol_contract_violated", "An impossible local or contract failure occurred.", protocol.BlockUntilExplicitRestartOrRelevantInputChange
	case kindCleanupFailed:
		code, description, recommendation = "logout_cleanup_failed", "Best-effort logout cleanup failed.", protocol.BlockUntilExplicitRestartOrRelevantInputChange
	}
	return &protocol.AuthenticationProtocolRunFailure{
		Code:                   protocol.AuthenticationProtocolFailureCode(code),
		Description:            description,
		HandlingRecommendation: recommendation,
		DiagnosticCause:        e,
	}
}

func networkTimeoutError(operation string, cause error) *runError {
	return &runError{operation: operation, kind: kindNetworkTimeout, cause: cause}
}

func networkIOError(operation string, cause error) *runError {
	return &runError{operation: operation, kind: kindNetworkIO, cause: cause}
}

func serverBusyExhaustedError(operation string, cause error) *runError {
	return &runError{operation: operation, kind: kindServerBusyExhausted, cause: cause}
}

func rejectionError(operation string, code byte, cause error) *runError {
	if _, ok := knownRejectionTable[code]; ok {
		return &runError{operation: operation, kind: kindKnownRejection, wireCode: code, hasWireCode: true, cause: cause}
	}
	return &runError{operation: operation, kind: kindUnknownRejection, wireCode: code, hasWireCode: true, cause: cause}
}

func responseInvalidError(operation string, cause error) *runError {
	return &runError{operation: operation, kind: kindResponseInvalid, cause: cause}
}

func contractViolationError(operation string, cause error) *runError {
	return &runError{operation: operation, kind: kindContractViolated, cause: cause}
}

func cleanupError(operation string, cause error) *runError {
	return &runError{operation: operation, kind: kindCleanupFailed, cause: cause}
}
