package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const settingsFileSizeLimit int64 = 1 * 1024 * 1024

type AutoConnectSettings struct {
	ConfigurationID config.ConfigurationID
}

type SettingsStore struct {
	mu          sync.Mutex
	store       jsonfile.Store
	path        string
	autoConnect *AutoConnectSettings
}

func OpenSettings(ctx context.Context, store jsonfile.Store, path string) (*SettingsStore, error) {
	if store == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, settingsInvalidArgument(nil)
	}
	if err := validateSettingsContext(ctx); err != nil {
		return nil, err
	}
	data, exists, err := store.Read(ctx, path, settingsFileSizeLimit)
	if err != nil {
		return nil, err
	}
	var autoConnect *AutoConnectSettings
	if exists {
		autoConnect, err = decodeSettingsDocument(data)
		if err != nil {
			return nil, err
		}
	}
	return &SettingsStore{store: store, path: path, autoConnect: autoConnect}, nil
}

func (store *SettingsStore) LoadAutoConnect(ctx context.Context) (AutoConnectSettings, bool, error) {
	if err := validateSettingsContext(ctx); err != nil {
		return AutoConnectSettings{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateSettingsContext(ctx); err != nil {
		return AutoConnectSettings{}, false, err
	}
	if store.autoConnect == nil {
		return AutoConnectSettings{}, false, nil
	}
	return *store.autoConnect, true, nil
}

func (store *SettingsStore) SaveAutoConnect(ctx context.Context, settings AutoConnectSettings) error {
	if settings.ConfigurationID == "" {
		return settingsInvalidArgument(nil)
	}
	if err := validateSettingsContext(ctx); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateSettingsContext(ctx); err != nil {
		return err
	}
	candidate := &AutoConnectSettings{ConfigurationID: settings.ConfigurationID}
	data, err := encodeSettingsDocument(candidate)
	if err != nil {
		return err
	}
	if err := store.store.Replace(ctx, store.path, data); err != nil {
		return err
	}
	store.autoConnect = candidate
	return nil
}

func (store *SettingsStore) ClearAutoConnect(ctx context.Context) error {
	if err := validateSettingsContext(ctx); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateSettingsContext(ctx); err != nil {
		return err
	}
	data, err := encodeSettingsDocument(nil)
	if err != nil {
		return err
	}
	if err := store.store.Replace(ctx, store.path, data); err != nil {
		return err
	}
	store.autoConnect = nil
	return nil
}

type settingsDocumentInput struct {
	SchemaVersion              *uint64                 `json:"schemaVersion"`
	AutoConnectConfigurationID *config.ConfigurationID `json:"autoConnectConfigurationId"`
}

type settingsDocumentOutput struct {
	SchemaVersion              uint64                  `json:"schemaVersion"`
	AutoConnectConfigurationID *config.ConfigurationID `json:"autoConnectConfigurationId"`
}

func decodeSettingsDocument(data []byte) (*AutoConnectSettings, error) {
	var document settingsDocumentInput
	if err := jsonfile.DecodeStrict(data, &document); err != nil {
		return nil, err
	}
	if document.SchemaVersion == nil {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("required settings field missing"))
	}
	if *document.SchemaVersion != jsonfile.SchemaVersion1 {
		return nil, persistence.NewFailure(persistence.FailureUnsupportedSchemaVersion, nil)
	}
	if document.AutoConnectConfigurationID == nil {
		return nil, nil
	}
	if *document.AutoConnectConfigurationID == "" {
		return nil, persistence.NewFailure(persistence.FailureInvalidDocument, fmt.Errorf("auto connect configuration id is empty"))
	}
	return &AutoConnectSettings{ConfigurationID: *document.AutoConnectConfigurationID}, nil
}

func encodeSettingsDocument(settings *AutoConnectSettings) ([]byte, error) {
	output := settingsDocumentOutput{
		SchemaVersion: jsonfile.SchemaVersion1,
	}
	if settings != nil {
		if settings.ConfigurationID == "" {
			return nil, persistence.NewFailure(persistence.FailureInvalidArgument, fmt.Errorf("auto connect configuration id is empty"))
		}
		id := settings.ConfigurationID
		output.AutoConnectConfigurationID = &id
	}
	return jsonfile.MarshalDeterministic(output)
}

func validateSettingsContext(ctx context.Context) error {
	if ctx == nil {
		return settingsInvalidArgument(nil)
	}
	if err := ctx.Err(); err != nil {
		return settingsInvalidArgument(err)
	}
	return nil
}

func settingsInvalidArgument(cause error) error {
	return persistence.NewFailure(persistence.FailureInvalidArgument, cause)
}
