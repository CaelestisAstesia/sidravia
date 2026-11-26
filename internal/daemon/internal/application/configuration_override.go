package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"sidravia/internal/connection/auth/protocol"
	connectionconfig "sidravia/internal/connection/config"
	profilevalue "sidravia/internal/connection/profile"
	"sidravia/internal/ipc/contract"
)

func overrideSchemaVersion(factory protocol.AuthenticationProtocolFactory) int {
	if capability, ok := factory.(protocol.ProtocolOverrideCapability); ok {
		return capability.OverrideSchemaVersion()
	}
	return 0
}

func (result ConfigurationResult) OverrideState() string {
	if result.Version == "" {
		return ""
	}
	raw := result.Configuration.ProtocolContextOverride
	if len(raw) == 0 {
		return "none"
	}
	switch result.RuntimeAvailability {
	case ConfigurationRuntimeOverrideInvalid:
		return "invalid"
	case ConfigurationRuntimeAvailable:
		if result.OverrideSchemaVersion == 0 {
			return "unavailable"
		}
		if !protocol.HasOverrideFields(raw) {
			return "empty"
		}
		return "enabled"
	default:
		return "unavailable"
	}
}

func configurationVersionConflict() *contract.Error {
	return &contract.Error{Code: contract.ErrorCodeConfigurationVersionConflict, Message: "configuration changed"}
}

func (application *Application) handleConfigurationOverride(ctx context.Context, method string, data json.RawMessage) (json.RawMessage, *contract.Error) {
	var result any
	switch method {
	case contract.MethodConfigurationGetOverride:
		request, err := contract.DecodeConfigurationReadOverridePayload(data)
		if err != nil {
			return nil, configurationInvalid()
		}
		value, err := application.GetConfiguration(ctx, connectionconfig.ID(request.ConfigurationID))
		if err != nil {
			return nil, configurationError(err)
		}
		view := contract.ConfigurationOverrideResult{Configuration: toConfigurationResult(value)}
		if request.ExpectedDocument != nil {
			// The catalog writes RawMessage using json.Marshal, which compacts
			// whitespace and escapes HTML characters. Compare in that same form.
			expected, expectedErr := json.Marshal(json.RawMessage(*request.ExpectedDocument))
			actual, actualErr := json.Marshal(json.RawMessage(value.Configuration.ProtocolContextOverride))
			matches := expectedErr == nil && actualErr == nil && bytes.Equal(expected, actual)
			view.MatchesDocument = &matches
		}
		if size := len(value.Configuration.ProtocolContextOverride); size > 0 && size <= contract.MaximumProtocolContextOverrideBytes {
			document := string(value.Configuration.ProtocolContextOverride)
			view.Document = &document
		}
		result = view
	case contract.MethodConfigurationValidateOverride:
		request, err := contract.DecodeConfigurationValidateOverridePayload(data)
		if err != nil {
			return nil, configurationInvalid()
		}
		profile, err := application.profiles.Get(ctx, profilevalue.ID(request.InstitutionProfileID))
		if err != nil {
			return nil, configurationError(NewResolutionFailure(ProfileNotFound, err))
		}
		factory, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID)
		if err != nil || overrideSchemaVersion(factory) == 0 {
			return nil, configurationError(NewResolutionFailure(ProtocolNotFound, err))
		}
		raw, err := contract.DecodeProtocolContextOverride([]byte(request.Document))
		if err == nil {
			err = factory.ValidateProtocolContextOverride(profilevalue.ProtocolOverride(raw))
		}
		validation := contract.ConfigurationOverrideValidationResult{Valid: err == nil}
		var field protocol.OverrideFieldError
		if errors.As(err, &field) {
			validation.Field = field.FieldName()
		}
		result = validation
	}
	wire, err := json.Marshal(result)
	// A legacy document can exceed the editor or transport limit. Keep the
	// configuration identity and version readable so it can still be cleared.
	if view, ok := result.(contract.ConfigurationOverrideResult); ok && len(wire) > contract.StateFrameLimit-1024 {
		view.Document = nil
		wire, err = json.Marshal(view)
	}
	if err != nil {
		return nil, configurationError(err)
	}
	return wire, nil
}
