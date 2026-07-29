package app

import (
	"context"
	"fmt"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/environment"
)

type ResolutionFailureCode string

const (
	ConfigurationNotFound ResolutionFailureCode = "configuration_not_found"
	ProfileNotFound       ResolutionFailureCode = "profile_not_found"
	ProtocolNotFound      ResolutionFailureCode = "protocol_not_found"
	InvalidConfiguration  ResolutionFailureCode = "invalid_configuration"
	InvalidEnvironment    ResolutionFailureCode = "invalid_environment"
)

type ResolutionFailure struct {
	code  ResolutionFailureCode
	cause error
}

func NewResolutionFailure(code ResolutionFailureCode, cause error) *ResolutionFailure {
	return &ResolutionFailure{code: code, cause: cause}
}

func (failure *ResolutionFailure) Error() string {
	if failure.cause != nil {
		return fmt.Sprintf("%s: %v", failure.code, failure.cause)
	}
	return string(failure.code)
}

func (failure *ResolutionFailure) Unwrap() error               { return failure.cause }
func (failure *ResolutionFailure) Code() ResolutionFailureCode { return failure.code }

type AuthenticationResolver struct {
	configurations *config.Catalog
	profiles       *config.ProfileCatalog
	protocols      *protocol.AuthenticationProtocolRegistry
	hostInfo       environment.SystemHostInformation
}

func NewAuthenticationResolver(
	configurations *config.Catalog,
	profiles *config.ProfileCatalog,
	protocols *protocol.AuthenticationProtocolRegistry,
	hostInfo environment.SystemHostInformation,
) (*AuthenticationResolver, error) {
	switch {
	case configurations == nil:
		return nil, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("configuration catalog is required"))
	case profiles == nil:
		return nil, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("profile catalog is required"))
	case protocols == nil:
		return nil, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("protocol registry is required"))
	case hostInfo.HostName == "":
		return nil, NewResolutionFailure(InvalidEnvironment, fmt.Errorf("system host information is required"))
	default:
		return &AuthenticationResolver{
			configurations: configurations,
			profiles:       profiles,
			protocols:      protocols,
			hostInfo:       hostInfo,
		}, nil
	}
}

func (authenticationResolver *AuthenticationResolver) Resolve(
	ctx context.Context,
	configurationID config.ConfigurationID,
	sessionID session.AuthenticationSessionID,
) (session.RuntimeDefinition, error) {
	if ctx == nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("context is required"))
	}
	if err := ctx.Err(); err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	if configurationID == "" {
		return session.RuntimeDefinition{}, NewResolutionFailure(ConfigurationNotFound, nil)
	}
	if sessionID == "" {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("authentication session ID is required"))
	}

	configuration, credentialValue, err := authenticationResolver.configurations.Resolve(ctx, configurationID)
	if err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(ConfigurationNotFound, err)
	}

	institutionProfile, err := authenticationResolver.profiles.Get(ctx, configuration.InstitutionProfileID)
	if err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(ProfileNotFound, err)
	}

	factory, err := authenticationResolver.protocols.GetFactory(institutionProfile.AuthenticationProtocolID)
	if err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(ProtocolNotFound, err)
	}

	definition := session.RuntimeDefinition{
		Configuration: session.Configuration{
			AuthenticationSessionID: sessionID,
			DisplayName:             configuration.DisplayName,
			InstitutionProfileID:    configuration.InstitutionProfileID,
			NetworkBindingPolicy: session.NetworkBindingPolicy{
				Mode: session.NetworkBindingPolicyMode(configuration.NetworkBindingPolicy.Mode),
			},
			ProtocolContextOverride: configuration.ProtocolContextOverride,
		},
		AuthenticationCredential:      credentialValue,
		InstitutionProfile:            institutionProfile,
		AuthenticationProtocolFactory: factory,
		SystemHostInformation:         authenticationResolver.hostInfo,
	}

	if err := definition.Validate(); err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, err)
	}

	return definition, nil
}

// OneShotAuthenticationInput is a typed one-shot authentication request. It
// carries a credential that the daemon never persists, logs, or exposes in a
// public Snapshot. It deliberately omits ConfigurationID,
// SessionID, arbitrary JSON maps, a protocol factory, and host or network
// facts: those are resolved from the existing catalogs and host information.
type OneShotAuthenticationInput struct {
	DisplayName              string
	InstitutionProfileID     config.InstitutionProfileID
	AuthenticationCredential credential.AuthenticationCredential
	NetworkBindingPolicy     session.NetworkBindingPolicy
	ProtocolContextOverride  protocol.AuthenticationProtocolContextOverride
}

// ResolveOneShot resolves a typed one-shot request into the existing
// RuntimeDefinition without reading or writing the Configuration Catalog or
// Catalog. The supplied credential is copied directly into the definition.
// The raw protocol override is cloned so the definition does not alias
// the caller's input.
func (authenticationResolver *AuthenticationResolver) ResolveOneShot(
	ctx context.Context,
	input OneShotAuthenticationInput,
	sessionID session.AuthenticationSessionID,
) (session.RuntimeDefinition, error) {
	if ctx == nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("context is required"))
	}
	if err := ctx.Err(); err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	if sessionID == "" {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("authentication session ID is required"))
	}
	if input.AuthenticationCredential.Username == "" {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, fmt.Errorf("username is required"))
	}

	institutionProfile, err := authenticationResolver.profiles.Get(ctx, input.InstitutionProfileID)
	if err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(ProfileNotFound, err)
	}

	factory, err := authenticationResolver.protocols.GetFactory(institutionProfile.AuthenticationProtocolID)
	if err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(ProtocolNotFound, err)
	}

	definition := session.RuntimeDefinition{
		Configuration: session.Configuration{
			AuthenticationSessionID: sessionID,
			DisplayName:             input.DisplayName,
			InstitutionProfileID:    institutionProfile.InstitutionProfileID,
			NetworkBindingPolicy:    input.NetworkBindingPolicy,
			ProtocolContextOverride: append(protocol.AuthenticationProtocolContextOverride(nil), input.ProtocolContextOverride...),
		},
		AuthenticationCredential:      input.AuthenticationCredential,
		InstitutionProfile:            institutionProfile,
		AuthenticationProtocolFactory: factory,
		SystemHostInformation:         authenticationResolver.hostInfo,
	}

	if err := definition.Validate(); err != nil {
		return session.RuntimeDefinition{}, NewResolutionFailure(InvalidConfiguration, err)
	}

	return definition, nil
}
