package configuration

import (
	"context"
	"crypto/rand"
	"fmt"
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
	ReplaceSensitive(context.Context, string, []byte, int64, bool) error
	ProtectionStatus() jsonfile.ProtectionStatus
}

type catalogRecord struct {
	configuration Configuration
	password      string
}

// AutoLoginConflict is returned when enabling AutoLogin would result in more
// than one automatic-login Configuration. The stable IPC code is
// configuration_auto_login_conflict.
type AutoLoginConflict struct{}

func (AutoLoginConflict) Error() string {
	return "another configuration already enables automatic login"
}

type Catalog struct {
	mu                 sync.Mutex
	store              SensitiveStore
	path               string
	records            map[ConfigurationID]catalogRecord
	unprotectedConsent bool
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

// HasRecord reports whether the catalog owns the Configuration aggregate. It
// deliberately does not expose the private password field.
func (catalog *Catalog) HasRecord(ctx context.Context, id ConfigurationID) (bool, error) {
	if !validConfigurationID(string(id)) {
		return false, catalogInvalidArgument(nil)
	}
	if err := validateCatalogContext(ctx); err != nil {
		return false, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return false, err
	}
	_, exists := catalog.records[id]
	return exists, nil
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

func (catalog *Catalog) Create(ctx context.Context, value Configuration, password string, allowUnprotected bool) (Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	if value.ConfigurationID == "" {
		var err error
		value.ConfigurationID, err = catalog.nextConfigurationID()
		if err != nil {
			return Configuration{}, err
		}
	}
	if err := value.Validate(); err != nil {
		return Configuration{}, catalogInvalidArgument(err)
	}
	if _, exists := catalog.records[value.ConfigurationID]; exists {
		return Configuration{}, persistence.NewFailure(persistence.FailureConflict, nil)
	}
	if value.AutoLogin {
		for _, existing := range catalog.records {
			if existing.configuration.AutoLogin {
				return Configuration{}, AutoLoginConflict{}
			}
		}
	}
	candidate := cloneRecords(catalog.records)
	candidate[value.ConfigurationID] = catalogRecord{configuration: value.Clone(), password: password}
	if err := catalog.commit(ctx, candidate, allowUnprotected); err != nil {
		return Configuration{}, err
	}
	return value.Clone(), nil
}

func (catalog *Catalog) nextConfigurationID() (ConfigurationID, error) {
	for range 8 {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return "", fmt.Errorf("generate configuration ID: %w", err)
		}
		candidate := ConfigurationID(fmt.Sprintf("cfg-%x", entropy))
		if _, exists := catalog.records[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", persistence.NewFailure(persistence.FailureConflict, nil)
}

type Update struct {
	Password             *string
	AllowInsecureStorage bool
	DisplayName          *string
	InstitutionProfileID *InstitutionProfileID
	Username             *string
	AutoLogin            *bool
	AutoReconnect        *bool
}

func (catalog *Catalog) Update(ctx context.Context, id ConfigurationID, update Update) (Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return Configuration{}, err
	}
	if !validConfigurationID(string(id)) || update.DisplayName == nil && update.InstitutionProfileID == nil && update.Username == nil && update.AutoLogin == nil && update.AutoReconnect == nil && update.Password == nil {
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
	if update.AutoLogin != nil {
		record.configuration.AutoLogin = *update.AutoLogin
	}
	if update.AutoReconnect != nil {
		record.configuration.AutoReconnect = *update.AutoReconnect
	}
	if update.Password != nil {
		record.password = *update.Password
	}
	if err := record.configuration.Validate(); err != nil {
		return Configuration{}, catalogInvalidArgument(err)
	}
	if record.configuration.AutoLogin {
		for existingID, existing := range catalog.records {
			if existingID != id && existing.configuration.AutoLogin {
				return Configuration{}, AutoLoginConflict{}
			}
		}
	}
	candidate := cloneRecords(catalog.records)
	candidate[id] = record
	if err := catalog.commit(ctx, candidate, update.AllowInsecureStorage || update.Password == nil && catalog.unprotectedConsent); err != nil {
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

func (catalog *Catalog) Delete(ctx context.Context, id ConfigurationID, allow ...bool) error {
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
	return catalog.commit(ctx, candidate, len(allow) > 0 && allow[0] || catalog.unprotectedConsent)
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
	// The store applies catalogFileSizeLimit to the exact bytes it persists and
	// re-reads, so a candidate that could not be read back is rejected before any
	// replacement and before the in-memory authority is committed.
	if err := catalog.store.ReplaceSensitive(ctx, catalog.path, data, catalogFileSizeLimit, allow); err != nil {
		return err
	}
	catalog.records = candidate
	catalog.unprotectedConsent = allow && catalog.store.ProtectionStatus() == jsonfile.ProtectionUnprotected
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
