package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"sidravia/internal/connection/binding"
	connectionconfig "sidravia/internal/connection/config"
	profilevalue "sidravia/internal/connection/profile"
	config "sidravia/internal/daemon/internal/application/internal/configstore"
	"sidravia/internal/document"
	"sidravia/internal/document/securefile"
	"sidravia/internal/ipc/contract"
)

type configurationApplication interface {
	ListConfigurations(context.Context) ([]ConfigurationResult, securefile.ProtectionStatus, error)
	GetConfiguration(context.Context, connectionconfig.ID) (ConfigurationResult, error)
	CreateConfiguration(context.Context, connectionconfig.Configuration, string, bool) (ConfigurationResult, error)
	UpdateConfiguration(context.Context, connectionconfig.ID, config.Update) (ConfigurationResult, error)
	SetConfigurationPassword(context.Context, connectionconfig.ID, string, bool) (ConfigurationResult, error)
	RemoveConfiguration(context.Context, connectionconfig.ID, string, ...bool) error
}

func dispatchConfiguration(application configurationApplication, ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	switch method {
	case contract.MethodConfigurationList:
		request, err := contract.DecodeConfigurationListPayload(payload)
		if err != nil {
			return nil, configurationInvalid()
		}
		values, protection, err := application.ListConfigurations(ctx)
		if err != nil {
			return nil, configurationError(err)
		}
		results := make([]contract.ConfigurationResult, 0, len(values))
		for _, value := range values {
			results = append(results, toConfigurationResult(value))
		}
		result, err := configurationPage(results, protection, request.Cursor)
		if err != nil {
			return nil, configurationError(err)
		}
		return result, nil
	case contract.MethodConfigurationGet:
		request, err := contract.DecodeConfigurationIDPayload(payload)
		if err != nil {
			return nil, configurationInvalid()
		}
		value, err := application.GetConfiguration(ctx, connectionconfig.ID(request.ConfigurationID))
		return encodeConfiguration(value, err)
	case contract.MethodConfigurationCreate:
		request, err := contract.DecodeConfigurationCreatePayload(payload)
		if err != nil {
			return nil, configurationInvalid()
		}
		policy, _ := request.NetworkBindingPolicy.Domain()
		value, err := application.CreateConfiguration(ctx, connectionconfig.Configuration{
			ProtocolContextOverride: profilevalue.ProtocolOverride(request.ProtocolContextOverride),
			ConfigurationID:         connectionconfig.ID(request.ConfigurationID),
			DisplayName:             request.DisplayName, InstitutionProfileID: profilevalue.ID(request.InstitutionProfileID),
			Username: request.Username, NetworkBindingPolicy: policy, AutoLogin: request.AutoLogin, AutoReconnect: request.AutoReconnect,
		}, request.Password, request.AllowInsecureStorage)
		return encodeConfiguration(value, err)
	case contract.MethodConfigurationUpdate:
		request, err := contract.DecodeConfigurationUpdatePayload(payload)
		if err != nil {
			return nil, configurationInvalid()
		}
		var policy *binding.Policy
		if request.NetworkBindingPolicy != nil {
			converted, _ := request.NetworkBindingPolicy.Domain()
			policy = &converted
		}
		var profile *profilevalue.ID
		if request.InstitutionProfileID != nil {
			converted := profilevalue.ID(*request.InstitutionProfileID)
			profile = &converted
		}
		var override *profilevalue.ProtocolOverride
		if len(request.ProtocolContextOverride) > 0 {
			var converted profilevalue.ProtocolOverride
			if !bytes.Equal(bytes.TrimSpace(request.ProtocolContextOverride), []byte("null")) {
				converted = append(converted, request.ProtocolContextOverride...)
			}
			override = &converted
		}
		value, err := application.UpdateConfiguration(ctx, connectionconfig.ID(request.ConfigurationID), config.Update{Patch: connectionconfig.Patch{ProtocolContextOverride: override, NetworkBindingPolicy: policy, DisplayName: request.DisplayName, InstitutionProfileID: profile, Username: request.Username, AutoLogin: request.AutoLogin, AutoReconnect: request.AutoReconnect}, ExpectedVersion: request.ExpectedVersion, Password: request.Password, AllowInsecureStorage: request.AllowInsecureStorage})
		return encodeConfiguration(value, err)
	case contract.MethodConfigurationSetPassword:
		request, err := contract.DecodeConfigurationSetPasswordPayload(payload)
		if err != nil {
			return nil, configurationInvalid()
		}
		value, err := application.SetConfigurationPassword(ctx, connectionconfig.ID(request.ConfigurationID), request.Password, request.AllowInsecureStorage)
		return encodeConfiguration(value, err)
	case contract.MethodConfigurationRemove:
		request, err := contract.DecodeConfigurationRemovePayload(payload)
		if err != nil {
			return nil, configurationInvalid()
		}
		if err := application.RemoveConfiguration(ctx, connectionconfig.ID(request.ConfigurationID), request.ExpectedVersion, request.AllowInsecureStorage); err != nil {
			return nil, configurationError(err)
		}
		result, err := contract.MarshalConfigurationRemoveResult(contract.ConfigurationRemoveResult{ConfigurationID: request.ConfigurationID, Status: "removed"})
		if err != nil {
			return nil, configurationError(err)
		}
		return result, nil
	default:
		return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported configuration method"}
	}
}

func toConfigurationResult(value ConfigurationResult) contract.ConfigurationResult {
	return contract.ConfigurationResult{
		Version: value.Version, OverrideState: value.OverrideState(), OverrideSchemaVersion: value.OverrideSchemaVersion, RuntimeAvailability: string(value.RuntimeAvailability),
		NetworkBindingPolicy: contract.NetworkBindingPolicyFromDomain(value.Configuration.NetworkBindingPolicy),
		ConfigurationID:      string(value.Configuration.ConfigurationID), DisplayName: value.Configuration.DisplayName,
		InstitutionProfileID: string(value.Configuration.InstitutionProfileID), InstitutionDisplayName: value.InstitutionDisplayName,
		AuthenticationProtocolID: value.AuthenticationProtocolID, Username: value.Configuration.Username,
		CredentialStored: value.CredentialStored, StorageProtection: string(value.StorageProtection),
		AutoLogin: value.Configuration.AutoLogin, AutoReconnect: value.Configuration.AutoReconnect,
	}
}
func encodeConfiguration(value ConfigurationResult, err error) (json.RawMessage, *contract.Error) {
	if err != nil {
		return nil, configurationError(err)
	}
	result, marshalErr := contract.MarshalConfigurationResult(toConfigurationResult(value))
	if marshalErr != nil {
		return nil, configurationError(marshalErr)
	}
	return result, nil
}
func configurationInvalid() *contract.Error {
	return &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "malformed configuration payload"}
}
func configurationError(err error) *contract.Error {
	if errors.Is(err, config.ErrVersionConflict) {
		return configurationVersionConflict()
	}
	if errors.Is(err, ErrConfigurationSessionInvalidation) {
		return &contract.Error{Code: contract.ErrorCodeConfigurationSessionInvalidationFailed, Message: "configuration committed; session cleanup required"}
	}
	var resolutionFailure *ResolutionFailure
	if errors.As(err, &resolutionFailure) {
		switch resolutionFailure.Code() {
		case ProfileNotFound:
			return &contract.Error{Code: contract.ErrorCodeProfileNotFound, Message: "institution profile not found"}
		case ProtocolNotFound:
			return &contract.Error{Code: contract.ErrorCodeProtocolNotFound, Message: "authentication protocol not found"}
		case InvalidConfiguration:
			return &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "invalid configuration"}
		}
	}
	var autoLoginConflict config.AutoLoginConflict
	if errors.As(err, &autoLoginConflict) {
		return &contract.Error{Code: contract.ErrorCodeConfigurationAutoLoginConflict, Message: "another configuration already enables automatic login"}
	}
	var failure *document.Failure
	if errors.As(err, &failure) {
		if failure.Code() == document.FailureNotFound {
			return &contract.Error{Code: contract.ErrorCodeConfigurationNotFound, Message: "configuration not found"}
		}
		if failure.Code() == document.FailureConflict {
			return &contract.Error{Code: contract.ErrorCodeConfigurationConflict, Message: "configuration already exists"}
		}
		if failure.Code() == document.FailureInvalidArgument {
			return &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "invalid configuration"}
		}
		if errors.Is(failure.DiagnosticCause(), securefile.ErrInsecureStorageConfirmationRequired) {
			return &contract.Error{Code: contract.ErrorCodeInsecureStorageConfirmationRequired, Message: "insecure storage confirmation required"}
		}
	}
	return &contract.Error{Code: contract.ErrorCodeConfigurationOperationFailed, Message: "configuration operation failed"}
}
