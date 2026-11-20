package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"sidravia/internal/credentialinput"
	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
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
	if err := application.validateConfigurationCandidate(ctx, value); err != nil {
		return ConfigurationResult{}, err
	}
	if _, err := application.enrich(ctx, value); err != nil {
		return ConfigurationResult{}, err
	}
	persisted, err := application.catalog.Create(ctx, value, password, allow)
	if err != nil {
		return ConfigurationResult{}, err
	}
	return application.enrich(ctx, persisted)
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
	var cleared ConfigurationResult
	if clearOnly {
		cleared, err = application.describeConfiguration(ctx, candidate)
		if err != nil {
			return ConfigurationResult{}, err
		}
	} else {
		if err := application.validateConfigurationCandidate(ctx, candidate); err != nil {
			return ConfigurationResult{}, err
		}
		if _, err := application.enrich(ctx, candidate); err != nil {
			return ConfigurationResult{}, err
		}
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
	if clearOnly {
		cleared.Configuration = value
		cleared.Version, err = application.catalog.Version(context.WithoutCancel(ctx), id)
		if err != nil {
			return ConfigurationResult{}, err
		}
		cleared.StorageProtection = application.catalog.StorageProtection()
		return cleared, nil
	}
	return application.enrich(ctx, value)
}

// ErrConfigurationSessionInvalidation distinguishes a durable edit from failed cleanup.
var ErrConfigurationSessionInvalidation = errors.New("configuration committed; session cleanup required")

func (application *Application) SetConfigurationPassword(ctx context.Context, id config.ConfigurationID, password string, allow bool) (ConfigurationResult, error) {
	return application.UpdateConfiguration(ctx, id, config.Update{Password: &password, AllowInsecureStorage: allow})
}

func (application *Application) retireConfigurationSession(ctx context.Context, id config.ConfigurationID) error {
	application.sessionIndexMu.Lock()
	sessionID := application.sessionsByConfig[id]
	application.sessionIndexMu.Unlock()
	if sessionID == "" {
		return nil
	}
	application.invalidSessions[sessionID] = true
	// Once persistence commits, caller cancellation must not skip cleanup.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	if err := application.supervisor.Remove(cleanup, sessionID); err != nil && !errors.Is(err, supervisor.ErrSessionNotFound) {
		return errors.Join(ErrConfigurationSessionInvalidation, err)
	}
	delete(application.invalidSessions, sessionID)
	application.sessionIndexMu.Lock()
	delete(application.sessionsByConfig, id)
	application.sessionIndexMu.Unlock()
	delete(application.reconnectByConfig, id)
	return nil
}

func (application *Application) RemoveConfiguration(ctx context.Context, id config.ConfigurationID, allow ...bool) error {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if err := application.catalog.Delete(ctx, id, allow...); err != nil {
		return err
	}
	return application.retireConfigurationSession(ctx, id)
}

func (application *Application) enrich(ctx context.Context, value config.Configuration) (ConfigurationResult, error) {
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
	stored := false
	version := ""
	if value.ConfigurationID != "" {
		var err error
		stored, err = application.catalog.HasRecord(ctx, value.ConfigurationID)
		if err != nil {
			return ConfigurationResult{}, err
		}
		if stored {
			version, err = application.catalog.Version(ctx, value.ConfigurationID)
			if err != nil {
				return ConfigurationResult{}, err
			}
		}
	}
	return ConfigurationResult{
		Version: version, OverrideSchemaVersion: overrideSchemaVersion(factory), RuntimeAvailability: ConfigurationRuntimeAvailable, Configuration: value.Clone(), InstitutionDisplayName: profile.DisplayName,
		AuthenticationProtocolID: string(profile.AuthenticationProtocolID),
		CredentialStored:         stored, StorageProtection: application.catalog.StorageProtection(),
	}, nil
}

// validateConfigurationCandidate is write-only validation of public candidate
// data. It never resolves stored credentials or creates a protocol Run, and
// leaves stored-row inspection to the tolerant describeConfiguration path.
func (application *Application) validateConfigurationCandidate(ctx context.Context, value config.Configuration) error {
	profile, err := application.profiles.Get(ctx, value.InstitutionProfileID)
	if err != nil {
		var failure *persistence.Failure
		if errors.As(err, &failure) && failure.Code() == persistence.FailureNotFound {
			return NewResolutionFailure(ProfileNotFound, err)
		}
		return NewResolutionFailure(InvalidConfiguration, err)
	}
	factory, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID)
	if err != nil {
		return NewResolutionFailure(ProtocolNotFound, err)
	}
	if err := factory.ValidateUsername(value.Username); err != nil {
		return NewResolutionFailure(InvalidConfiguration, err)
	}
	if err := factory.ValidateProtocolContextOverride(append(protocol.AuthenticationProtocolContextOverride(nil), value.ProtocolContextOverride...)); err != nil {
		return NewResolutionFailure(InvalidConfiguration, err)
	}
	return nil
}
