package protocol

import (
	"context"
	"encoding/json"
	"net/netip"

	credential "sidravia/internal/connection/credential"
	environment "sidravia/internal/connection/environment"
	profilevalue "sidravia/internal/connection/profile"
)

type AuthenticationProtocolFailureCode string

type AuthenticationProtocolRunCreationInputs struct {
	InstitutionProtocolConfiguration profilevalue.ProtocolConfiguration
	AuthenticationCredential         credential.AuthenticationCredential
	SelectedSystemNetworkBinding     environment.SelectedSystemNetworkBinding
	SystemHostInformation            environment.SystemHostInformation
	ProtocolContextOverride          profilevalue.ProtocolOverride
	Diagnostics                      AuthenticationProtocolDiagnostics
}

type AuthenticationProtocolFactory interface {
	ProtocolID() profilevalue.ProtocolID
	ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error
	ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error
	// ValidateUsername checks public candidate data without credentials, a Run or I/O.
	ValidateUsername(string) error
	CreateAuthenticationProtocolRun(AuthenticationProtocolRunCreationInputs) (AuthenticationProtocolRun, error)
}

// AuthenticationProtocolRunNetworkObserver receives actual owned socket facts.
// It is optional and separate from the logging diagnostics sink.
type AuthenticationProtocolRunNetworkObserver interface {
	ProtocolSocketOpened(local, remote netip.AddrPort) error
	ProtocolSocketClosed(closed bool)
}

type AuthenticationProtocolRunObserver interface {
	AuthenticationEstablished()
}

type AuthenticationProtocolRun interface {
	Execute(context.Context, AuthenticationProtocolRunObserver) *AuthenticationProtocolRunFailure
}

// AuthenticationProtocolDatagramDirection is the direction of a protocol
// datagram relative to the authentication server: tx (sent) or rx (received).
type AuthenticationProtocolDatagramDirection string

const (
	DatagramDirectionTx AuthenticationProtocolDatagramDirection = "tx"
	DatagramDirectionRx AuthenticationProtocolDatagramDirection = "rx"
)

// AuthenticationProtocolDiagnostics observes protocol-run events without
// altering protocol behavior. It is transport-neutral: no slog type crosses
// this boundary into Session, Supervisor or D520. The sink has no return value
// and cannot change protocol decisions, retry, cancellation, return values,
// IPC responses or shutdown ordering. Diagnostic errors never propagate.
type AuthenticationProtocolDiagnostics interface {
	// PhaseEvent records a protocol phase boundary at Debug level. boundary is
	// a fixed "begin" or "end" marker. phase is a stable protocol phase string
	// supplied by the run, never derived from packet contents.
	PhaseEvent(phase, boundary string)
	// DatagramEvent records one complete UDP datagram at Trace level. The
	// production adapter renders the complete lowercase datagram hex; the
	// protocol run passes the raw bytes and a stable phase without deriving
	// fields from packet contents.
	DatagramEvent(phase string, direction AuthenticationProtocolDatagramDirection, datagram []byte)
}

// AuthenticationProtocolExchangeOutcome is a safe, fixed diagnostic category.
// It contains no packet contents or peer-provided error text.
type AuthenticationProtocolExchangeOutcome string

const (
	ExchangeObserved         AuthenticationProtocolExchangeOutcome = "observed"
	ExchangeControlReceived  AuthenticationProtocolExchangeOutcome = "control_received"
	ExchangeAccepted         AuthenticationProtocolExchangeOutcome = "accepted"
	ExchangeStaleIgnored     AuthenticationProtocolExchangeOutcome = "stale_ignored"
	ExchangeInvalidLength    AuthenticationProtocolExchangeOutcome = "invalid_length"
	ExchangeInvalidOpcode    AuthenticationProtocolExchangeOutcome = "invalid_opcode"
	ExchangeInvalidSequence  AuthenticationProtocolExchangeOutcome = "invalid_sequence"
	ExchangeInvalidType      AuthenticationProtocolExchangeOutcome = "invalid_type"
	ExchangeInvalidStructure AuthenticationProtocolExchangeOutcome = "invalid_structure"
	ExchangeNetworkTimeout   AuthenticationProtocolExchangeOutcome = "network_timeout"
	ExchangeNetworkIOFailed  AuthenticationProtocolExchangeOutcome = "network_io_failed"
)

// AuthenticationProtocolExchangeDiagnostics optionally observes classified
// exchange outcomes. Existing phase/Trace sinks need not implement this hook.
type AuthenticationProtocolExchangeDiagnostics interface {
	ExchangeOutcome(phase string, outcome AuthenticationProtocolExchangeOutcome, length int)
}

// AuthenticationProtocolDecisionDiagnostics optionally observes a fixed protocol
// decision name and a bounded numeric value. Neither carries server text,
// addresses, credentials, salts or packet contents.
type AuthenticationProtocolDecisionDiagnostics interface {
	DecisionEvent(decision string, value int)
}

// AuthenticationProtocolNetworkFailure contains only socket facts, fixed
// operation names and numeric native status. It never contains raw error text.
type AuthenticationProtocolNetworkFailure struct {
	Operation      string
	SystemCall     string
	SystemCode     uint64
	TimedOut       bool
	LocalEndpoint  netip.AddrPort
	RemoteEndpoint netip.AddrPort
}

// AuthenticationProtocolNetworkDiagnostics passively records socket failures for logs.
// It does not initiate independent network checks or probes.
// Consumers decide whether to enable expensive diagnostic collection.
type AuthenticationProtocolNetworkDiagnostics interface {
	NetworkFailure(AuthenticationProtocolNetworkFailure)
}

// NoopAuthenticationProtocolDiagnostics is the explicit no-op implementation
// used when diagnostics are disabled. It replaces scattered nil checks in the
// protocol run.
type NoopAuthenticationProtocolDiagnostics struct{}

func (NoopAuthenticationProtocolDiagnostics) PhaseEvent(string, string) {}

func (NoopAuthenticationProtocolDiagnostics) DatagramEvent(string, AuthenticationProtocolDatagramDirection, []byte) {
}

// ProtocolOverrideCapability identifies factories with an editable JSON override.
type ProtocolOverrideCapability interface {
	OverrideSchemaVersion() int
}

// OverrideFieldError identifies an invalid field from the protocol's own
// whitelist. It never contains a supplied value or an unknown member name.
type OverrideFieldError interface {
	error
	FieldName() string
}

// HasOverrideFields distinguishes a version-only document from a real override.
// It is used for safe status projection, never to validate a stored document.
func HasOverrideFields(raw profilevalue.ProtocolOverride) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	delete(fields, "schemaVersion")
	return len(fields) != 0
}
