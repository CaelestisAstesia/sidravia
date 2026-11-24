package app

import (
	"context"
	"errors"
	"reflect"

	"sidravia/internal/credentialinput"
	"sidravia/internal/daemon/authentication/protocol"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/persistence"
	"sidravia/internal/persistence/jsonfile"
)

type ConfigurationResult struct {
	Version                  string
	OverrideSchemaVersion    int
	RuntimeAvailability      ConfigurationRuntimeAvailability
	Configuration            config.Configuration
	InstitutionDisplayName   string
	AuthenticationProtocolID string
	CredentialStored         bool
	StorageProtection        jsonfile.ProtectionStatus
}

func (application *Application) ListConfigurations(ctx context.Context) ([]ConfigurationResult, jsonfile.ProtectionStatus, error) {
	if err := readModelContextError(ctx); err != nil {
		return nil, "", err
	}
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if err := readModelContextError(ctx); err != nil {
		return nil, "", err
	}
	values, err := application.catalog.List(ctx)
	if err != nil {
		if contextErr := readModelContextError(ctx); contextErr != nil {
			err = errors.Join(err, contextErr)
		}
		return nil, "", err
	}
	results := make([]ConfigurationResult, 0, len(values))
	for _, value := range values {
		result, err := application.describeConfiguration(ctx, value)
		if err != nil {
			return nil, "", err
		}
		results = append(results, result)
	}
	if err := readModelContextError(ctx); err != nil {
		return nil, "", err
	}
	return results, application.catalog.StorageProtection(), nil
}

func (application *Application) GetConfiguration(ctx context.Context, id config.ConfigurationID) (ConfigurationResult, error) {
	if err := readModelContextError(ctx); err != nil {
		return ConfigurationResult{}, err
	}
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if err := readModelContextError(ctx); err != nil {
		return ConfigurationResult{}, err
	}
	value, err := application.catalog.Get(ctx, id)
	if err != nil {
		if contextErr := readModelContextError(ctx); contextErr != nil {
			err = errors.Join(err, contextErr)
		}
		return ConfigurationResult{}, err
	}
	return application.describeConfiguration(ctx, value)
}

func (application *Application) CreateConfiguration(ctx context.Context, value config.Configuration, password string, allow bool) (ConfigurationResult, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	value = value.Clone()
	if err := credentialinput.Validate(value.Username); err != nil {
		return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	if err := credentialinput.Validate(password); err != nil {
		return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	receipt, err := application.prepareConfigurationWrite(ctx, value)
	if err != nil {
		return ConfigurationResult{}, err
	}
	persisted, err := application.catalog.Create(ctx, value, password, allow)
	if err != nil {
		return ConfigurationResult{}, err
	}
	return application.completeConfigurationWrite(ctx, receipt, persisted)
}

func (application *Application) UpdateConfiguration(ctx context.Context, id config.ConfigurationID, update config.Update) (ConfigurationResult, error) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	current, err := application.catalog.Get(ctx, id)
	if err != nil {
		return ConfigurationResult{}, err
	}
	if update.ExpectedVersion != "" {
		version, err := application.catalog.Version(ctx, id)
		if err != nil {
			return ConfigurationResult{}, err
		}
		if version != update.ExpectedVersion {
			return ConfigurationResult{}, config.ErrVersionConflict
		}
	}
	// Only newly changed credentials are validated; unrelated edits retain a
	// legacy username verbatim, including an unchanged value sent by older GUIs.
	if update.Username != nil && *update.Username != current.Username {
		if err := credentialinput.Validate(*update.Username); err != nil {
			return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
		}
	}
	if update.Password != nil {
		if err := credentialinput.Validate(*update.Password); err != nil {
			return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
		}
	}
	candidate := current.Clone()
	clearOnly := update.ProtocolContextOverride != nil && len(*update.ProtocolContextOverride) == 0 &&
		update.DisplayName == nil && update.InstitutionProfileID == nil && update.Username == nil &&
		update.AutoLogin == nil && update.AutoReconnect == nil && update.Password == nil && update.NetworkBindingPolicy == nil
	if clearOnly && len(current.ProtocolContextOverride) == 0 {
		return application.describeConfiguration(ctx, current)
	}
	if update.ProtocolContextOverride != nil {
		candidate.ProtocolContextOverride = append(protocol.AuthenticationProtocolContextOverride(nil), (*update.ProtocolContextOverride)...)
	}
	if update.NetworkBindingPolicy != nil {
		candidate.NetworkBindingPolicy = *update.NetworkBindingPolicy
	}
	if update.DisplayName != nil {
		candidate.DisplayName = *update.DisplayName
	}
	if update.InstitutionProfileID != nil {
		candidate.InstitutionProfileID = *update.InstitutionProfileID
	}
	if update.Username != nil {
		candidate.Username = *update.Username
	}
	if update.AutoLogin != nil {
		candidate.AutoLogin = *update.AutoLogin
	}
	if update.AutoReconnect != nil {
		candidate.AutoReconnect = *update.AutoReconnect
	}
	// Removing only an override is a recovery operation even if the saved
	// Profile or protocol cannot be resolved. The catalog still validates and
	// commits the configuration using its ordinary storage protections.
	var receipt ConfigurationResult
	if clearOnly {
		receipt, err = application.describeConfiguration(ctx, candidate)
	} else {
		receipt, err = application.prepareConfigurationWrite(ctx, candidate)
	}
	if err != nil {
		return ConfigurationResult{}, err
	}
	value, err := application.catalog.Update(ctx, id, update)
	if err != nil {
		return ConfigurationResult{}, err
	}
	// Display labels, logon and reconnect policy do not affect the current
	// immutable runtime. Reconnect policy is captured at the next manual
	// resume or restart instead.
	current.DisplayName, candidate.DisplayName = "", ""
	current.AutoLogin, candidate.AutoLogin = false, false
	current.AutoReconnect, candidate.AutoReconnect = false, false
	if update.Password != nil || !reflect.DeepEqual(current, candidate) {
		if err := application.retireConfigurationSession(ctx, id); err != nil {
			return ConfigurationResult{}, err
		}
	}
	return application.completeConfigurationWrite(ctx, receipt, value)
}

func (application *Application) SetConfigurationPassword(ctx context.Context, id config.ConfigurationID, password string, allow bool) (ConfigurationResult, error) {
	return application.UpdateConfiguration(ctx, id, config.Update{Password: &password, AllowInsecureStorage: allow})
}

// prepareConfigurationWrite validates the public candidate and resolves the
// receipt metadata before committing. It never reads credentials or starts a Run;
// stored-row inspection uses the tolerant describeConfiguration path.
func (application *Application) prepareConfigurationWrite(ctx context.Context, value config.Configuration) (ConfigurationResult, error) {
	profile, err := application.profiles.Get(ctx, value.InstitutionProfileID)
	if err != nil {
		var failure *persistence.Failure
		if errors.As(err, &failure) && failure.Code() == persistence.FailureNotFound {
			return ConfigurationResult{}, NewResolutionFailure(ProfileNotFound, err)
		}
		return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	factory, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID)
	if err != nil {
		return ConfigurationResult{}, NewResolutionFailure(ProtocolNotFound, err)
	}
	if err := factory.ValidateUsername(value.Username); err != nil {
		return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	if err := factory.ValidateProtocolContextOverride(append(protocol.AuthenticationProtocolContextOverride(nil), value.ProtocolContextOverride...)); err != nil {
		return ConfigurationResult{}, NewResolutionFailure(InvalidConfiguration, err)
	}
	return ConfigurationResult{
		OverrideSchemaVersion:    overrideSchemaVersion(factory),
		RuntimeAvailability:      ConfigurationRuntimeAvailable,
		InstitutionDisplayName:   profile.DisplayName,
		AuthenticationProtocolID: string(profile.AuthenticationProtocolID),
	}, nil
}

// completeConfigurationWrite only reads in-memory metadata under operationMu.
// Cancellation after persistence must not turn a committed edit into a
// validation failure. A disconnected client can still miss the receipt.
func (application *Application) completeConfigurationWrite(ctx context.Context, receipt ConfigurationResult, value config.Configuration) (ConfigurationResult, error) {
	version, err := application.catalog.Version(context.WithoutCancel(ctx), value.ConfigurationID)
	if err != nil {
		return ConfigurationResult{}, err
	}
	receipt.Configuration = value
	receipt.Version = version
	receipt.CredentialStored = true
	receipt.StorageProtection = application.catalog.StorageProtection()
	return receipt, nil
}
