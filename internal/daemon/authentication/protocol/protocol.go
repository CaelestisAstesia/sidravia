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
