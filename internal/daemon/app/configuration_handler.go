package app

import (
	"context"
	"encoding/json"
	"errors"

	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
)

type configurationApplication interface {
	ListConfigurations(context.Context) ([]ConfigurationResult, jsonfile.ProtectionStatus, error)
	GetConfiguration(context.Context, config.ConfigurationID) (ConfigurationResult, error)
	CreateConfiguration(context.Context, config.Configuration, string, bool) (ConfigurationResult, error)
	UpdateConfiguration(context.Context, config.ConfigurationID, config.Update) (ConfigurationResult, error)
	SetConfigurationPassword(context.Context, config.ConfigurationID, string, bool) (ConfigurationResult, error)
	RemoveConfiguration(context.Context, config.ConfigurationID) error
}

func ConfigurationHandler(application configurationApplication) func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		switch method {
		case contract.MethodConfigurationList:
			if err := contract.DecodeEmptyPayload(payload); err != nil {
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
			result, err := contract.MarshalConfigurationListResult(contract.ConfigurationListResult{StorageProtection: string(protection), Configurations: results})
			if err != nil {
				return nil, configurationError(err)
			}
			return result, nil
		case contract.MethodConfigurationGet:
			request, err := contract.DecodeConfigurationIDPayload(payload)
			if err != nil {
				return nil, configurationInvalid()
			}
			value, err := application.GetConfiguration(ctx, config.ConfigurationID(request.ConfigurationID))
			return encodeConfiguration(value, err)
		case contract.MethodConfigurationCreate:
			request, err := contract.DecodeConfigurationCreatePayload(payload)
			if err != nil {
				return nil, configurationInvalid()
			}
			value, err := application.CreateConfiguration(ctx, config.Configuration{
				ConfigurationID: config.ConfigurationID(request.ConfigurationID),
				DisplayName:     request.DisplayName, InstitutionProfileID: config.InstitutionProfileID(request.InstitutionProfileID),
				Username: request.Username, NetworkBindingPolicy: config.NetworkBindingPolicy{Mode: config.AutomaticallySelectLatestAvailable}, AutoLogin: request.AutoLogin, AutoReconnect: request.AutoReconnect,
			}, request.Password, request.AllowInsecureStorage)
			return encodeConfiguration(value, err)
		case contract.MethodConfigurationUpdate:
			request, err := contract.DecodeConfigurationUpdatePayload(payload)
			if err != nil {
				return nil, configurationInvalid()
			}
			var profile *config.InstitutionProfileID
			if request.InstitutionProfileID != nil {
				converted := config.InstitutionProfileID(*request.InstitutionProfileID)
				profile = &converted
			}
			value, err := application.UpdateConfiguration(ctx, config.ConfigurationID(request.ConfigurationID), config.Update{DisplayName: request.DisplayName, InstitutionProfileID: profile, Username: request.Username, AutoLogin: request.AutoLogin, AutoReconnect: request.AutoReconnect})
			return encodeConfiguration(value, err)
		case contract.MethodConfigurationSetPassword:
			request, err := contract.DecodeConfigurationSetPasswordPayload(payload)
			if err != nil {
				return nil, configurationInvalid()
			}
			value, err := application.SetConfigurationPassword(ctx, config.ConfigurationID(request.ConfigurationID), request.Password, request.AllowInsecureStorage)
			return encodeConfiguration(value, err)
		case contract.MethodConfigurationRemove:
			request, err := contract.DecodeConfigurationIDPayload(payload)
			if err != nil {
				return nil, configurationInvalid()
			}
			if err := application.RemoveConfiguration(ctx, config.ConfigurationID(request.ConfigurationID)); err != nil {
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
}

func toConfigurationResult(value ConfigurationResult) contract.ConfigurationResult {
	return contract.ConfigurationResult{
		ConfigurationID: string(value.Configuration.ConfigurationID), DisplayName: value.Configuration.DisplayName,
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
	var autoLoginConflict config.AutoLoginConflict
	if errors.As(err, &autoLoginConflict) {
		return &contract.Error{Code: contract.ErrorCodeConfigurationAutoLoginConflict, Message: "another configuration already enables automatic login"}
	}
	var failure *persistence.Failure
	if errors.As(err, &failure) {
		if failure.Code() == persistence.FailureNotFound {
			return &contract.Error{Code: contract.ErrorCodeConfigurationNotFound, Message: "configuration not found"}
		}
		if failure.Code() == persistence.FailureConflict {
			return &contract.Error{Code: contract.ErrorCodeConfigurationConflict, Message: "configuration already exists"}
		}
		if errors.Is(failure.DiagnosticCause(), jsonfile.ErrInsecureStorageConfirmationRequired) {
			return &contract.Error{Code: contract.ErrorCodeInsecureStorageConfirmationRequired, Message: "insecure storage confirmation required"}
		}
	}
	return &contract.Error{Code: contract.ErrorCodeConfigurationOperationFailed, Message: "configuration operation failed"}
}
