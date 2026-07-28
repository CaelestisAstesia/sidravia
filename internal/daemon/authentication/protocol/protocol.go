package protocol

import (
	"context"
	"encoding/json"

	credential "sidravia/internal/daemon/credentials"
	environment "sidravia/internal/daemon/environment"
)

type AuthenticationProtocolID string
type AuthenticationProtocolFailureCode string
type InstitutionProtocolConfiguration json.RawMessage
type AuthenticationProtocolContextOverride json.RawMessage

type AuthenticationProtocolRunCreationInputs struct {
	InstitutionProtocolConfiguration InstitutionProtocolConfiguration
	AuthenticationCredential         credential.AuthenticationCredential
	SelectedSystemNetworkBinding     environment.SelectedSystemNetworkBinding
	SystemHostInformation            environment.SystemHostInformation
	ProtocolContextOverride          AuthenticationProtocolContextOverride
	Diagnostics                      AuthenticationProtocolDiagnostics
}

type AuthenticationProtocolFactory interface {
	ProtocolID() AuthenticationProtocolID
	ValidateInstitutionProtocolConfiguration(InstitutionProtocolConfiguration) error
	ValidateProtocolContextOverride(AuthenticationProtocolContextOverride) error
	CreateAuthenticationProtocolRun(AuthenticationProtocolRunCreationInputs) (AuthenticationProtocolRun, error)
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

// NoopAuthenticationProtocolDiagnostics is the explicit no-op implementation
// used when diagnostics are disabled. It replaces scattered nil checks in the
// protocol run.
type NoopAuthenticationProtocolDiagnostics struct{}

func (NoopAuthenticationProtocolDiagnostics) PhaseEvent(string, string) {}

func (NoopAuthenticationProtocolDiagnostics) DatagramEvent(string, AuthenticationProtocolDatagramDirection, []byte) {
}
