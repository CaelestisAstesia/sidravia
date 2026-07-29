package configuration

import (
	"context"
	"path/filepath"
	"sort"
	"sync"

	"sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const catalogFileSizeLimit int64 = 1 * 1024 * 1024

type SensitiveStore interface {
	jsonfile.Store
	ReplaceSensitive(context.Context, string, []byte, bool) error
	ProtectionStatus() jsonfile.ProtectionStatus
}

type catalogRecord struct {
	configuration Configuration
	password      string
}

type Catalog struct {
	mu      sync.Mutex
	store   SensitiveStore
	path    string
	records map[ConfigurationID]catalogRecord
}

func OpenCatalog(ctx context.Context, store SensitiveStore, path string) (*Catalog, error) {
	if store == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, catalogInvalidArgument(nil)
	}
	if err := validateCatalogContext(ctx); err != nil {
		return nil, err
	}
	data, exists, err := store.Read(ctx, path, catalogFileSizeLimit)
	if err != nil {
		return nil, err
	}
	records := make(map[ConfigurationID]catalogRecord)
	if exists {
		records, err = decodeCatalogDocument(data)
		if err != nil {
			return nil, err
		}
	}
	return &Catalog{store: store, path: path, records: records}, nil
}

func (catalog *Catalog) Get(ctx context.Context, id ConfigurationID) (Configuration, error) {
	if !validConfigurationID(string(id)) {
		return Configuration{}, catalogInvalidArgument(nil)
	}
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return Configuration{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return record.configuration.Clone(), nil
}

func (catalog *Catalog) List(ctx context.Context) ([]Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return nil, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return nil, err
	}
	ids := make([]ConfigurationID, 0, len(catalog.records))
	for id := range catalog.records {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]Configuration, 0, len(ids))
	for _, id := range ids {
		result = append(result, catalog.records[id].configuration.Clone())
	}
	return result, nil
}

func (catalog *Catalog) Create(ctx context.Context, value Configuration, password string, allowUnprotected bool) error {
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return catalogInvalidArgument(err)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if _, exists := catalog.records[value.ConfigurationID]; exists {
		return persistence.NewFailure(persistence.FailureConflict, nil)
	}
	candidate := cloneRecords(catalog.records)
	candidate[value.ConfigurationID] = catalogRecord{configuration: value.Clone(), password: password}
	return catalog.commit(ctx, candidate, allowUnprotected)
}

type Update struct {
	DisplayName          *string
	InstitutionProfileID *InstitutionProfileID
	Username             *string
}

func (catalog *Catalog) Update(ctx context.Context, id ConfigurationID, update Update) (Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	if !validConfigurationID(string(id)) || update.DisplayName == nil && update.InstitutionProfileID == nil && update.Username == nil {
		return Configuration{}, catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return Configuration{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	if update.DisplayName != nil {
		record.configuration.DisplayName = *update.DisplayName
	}
	if update.InstitutionProfileID != nil {
		record.configuration.InstitutionProfileID = *update.InstitutionProfileID
	}
	if update.Username != nil {
		record.configuration.Username = *update.Username
	}
	if err := record.configuration.Validate(); err != nil {
		return Configuration{}, catalogInvalidArgument(err)
	}
	candidate := cloneRecords(catalog.records)
	candidate[id] = record
	if err := catalog.commit(ctx, candidate, true); err != nil {
		return Configuration{}, err
	}
	return record.configuration.Clone(), nil
}

func (catalog *Catalog) SetPassword(ctx context.Context, id ConfigurationID, password string, allowUnprotected bool) (Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	if !validConfigurationID(string(id)) {
		return Configuration{}, catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return Configuration{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	record.password = password
	candidate := cloneRecords(catalog.records)
	candidate[id] = record
	if err := catalog.commit(ctx, candidate, allowUnprotected); err != nil {
		return Configuration{}, err
	}
	return record.configuration.Clone(), nil
}

func (catalog *Catalog) Delete(ctx context.Context, id ConfigurationID) error {
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if !validConfigurationID(string(id)) {
		return catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if _, exists := catalog.records[id]; !exists {
		return persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	candidate := cloneRecords(catalog.records)
	delete(candidate, id)
	return catalog.commit(ctx, candidate, true)
}

func (catalog *Catalog) Resolve(ctx context.Context, id ConfigurationID) (Configuration, credentials.AuthenticationCredential, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, credentials.AuthenticationCredential{}, err
	}
	if !validConfigurationID(string(id)) {
		return Configuration{}, credentials.AuthenticationCredential{}, catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, credentials.AuthenticationCredential{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return Configuration{}, credentials.AuthenticationCredential{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return record.configuration.Clone(), credentials.AuthenticationCredential{Username: record.configuration.Username, Password: record.password}, nil
}

func (catalog *Catalog) StorageProtection() jsonfile.ProtectionStatus {
	return catalog.store.ProtectionStatus()
}

func (catalog *Catalog) commit(ctx context.Context, candidate map[ConfigurationID]catalogRecord, allow bool) error {
	data, err := encodeCatalogDocument(candidate)
	if err != nil {
		return err
	}
	if err := catalog.store.ReplaceSensitive(ctx, catalog.path, data, allow); err != nil {
		return err
	}
	catalog.records = candidate
	return nil
}

func cloneRecords(source map[ConfigurationID]catalogRecord) map[ConfigurationID]catalogRecord {
	result := make(map[ConfigurationID]catalogRecord, len(source))
	for id, record := range source {
		record.configuration = record.configuration.Clone()
		result[id] = record
	}
	return result
}

func validateCatalogContext(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		var cause error
		if ctx != nil {
			cause = ctx.Err()
		}
		return catalogInvalidArgument(cause)
	}
	return nil
}

func catalogInvalidArgument(cause error) error {
	return persistence.NewFailure(persistence.FailureInvalidArgument, cause)
}
