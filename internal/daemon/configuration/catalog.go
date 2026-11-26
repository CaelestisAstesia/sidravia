package configuration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	connectionconfig "sidravia/internal/connection/config"
	"sidravia/internal/connection/credential"
	"sidravia/internal/document"
	"sidravia/internal/document/securefile"
)

const catalogFileSizeLimit int64 = 1 * 1024 * 1024

type SensitiveStore interface {
	securefile.Store
	ReplaceSensitive(context.Context, string, []byte, int64, bool) error
	PreserveSensitive(context.Context, string, []byte, int64, bool) error
	ProtectionStatus() securefile.ProtectionStatus
}

type catalogRecord struct {
	configuration connectionconfig.Configuration
	password      string
	version       string
}

// AutoLoginConflict is returned when enabling AutoLogin would result in more
// than one automatic-login Configuration. The stable IPC code is
// configuration_auto_login_conflict.
type AutoLoginConflict struct{}

func (AutoLoginConflict) Error() string {
	return "another configuration already enables automatic login"
}

// Catalog owns saved configurations and their passwords as one persisted aggregate.
// Get and List return public configuration copies; only Resolve returns credential.
type Catalog struct {
	mu                 sync.Mutex // serializes record updates, persistence and migration/consent state
	store              SensitiveStore
	path               string
	records            map[connectionconfig.ID]catalogRecord
	unprotectedConsent bool
	migrationOriginal  []byte
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
	records := make(map[connectionconfig.ID]catalogRecord)
	if exists {
		records, err = decodeCatalogDocument(data)
		if err != nil {
			return nil, err
		}
	}
	for id, record := range records {
		version, err := newConfigurationVersion()
		if err != nil {
			return nil, err
		}
		record.version = version
		records[id] = record
	}
	catalog := &Catalog{store: store, path: path, records: records}
	if exists {
		// decodeCatalogDocument already validated this envelope and version.
		var envelope catalogDocumentEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, document.NewFailure(document.FailureInvalidDocument, err)
		}
		if *envelope.SchemaVersion != catalogSchemaVersion {
			catalog.migrationOriginal = append([]byte(nil), data...)
		}
	}
	return catalog, nil
}

func (catalog *Catalog) Get(ctx context.Context, id connectionconfig.ID) (connectionconfig.Configuration, error) {
	if !id.Valid() {
		return connectionconfig.Configuration{}, catalogInvalidArgument(nil)
	}
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return connectionconfig.Configuration{}, document.NewFailure(document.FailureNotFound, nil)
	}
	return record.configuration.Clone(), nil
}

// Version is an opaque edit baseline valid only for this catalog generation.
func (catalog *Catalog) Version(ctx context.Context, id connectionconfig.ID) (string, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return "", err
	}
	record, ok := catalog.records[id]
	if !ok {
		return "", document.NewFailure(document.FailureNotFound, nil)
	}
	return record.version, nil
}

func newConfigurationVersion() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", entropy), nil
}

// HasRecord reports whether the catalog owns the Configuration aggregate. It
// deliberately does not expose the private password field.
func (catalog *Catalog) HasRecord(ctx context.Context, id connectionconfig.ID) (bool, error) {
	if !id.Valid() {
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

func (catalog *Catalog) List(ctx context.Context) ([]connectionconfig.Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return nil, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return nil, err
	}
	ids := make([]connectionconfig.ID, 0, len(catalog.records))
	for id := range catalog.records {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]connectionconfig.Configuration, 0, len(ids))
	for _, id := range ids {
		result = append(result, catalog.records[id].configuration.Clone())
	}
	return result, nil
}

func (catalog *Catalog) Create(ctx context.Context, value connectionconfig.Configuration, password string, allowUnprotected bool) (connectionconfig.Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	if value.ConfigurationID == "" {
		var err error
		value.ConfigurationID, err = catalog.nextConfigurationID()
		if err != nil {
			return connectionconfig.Configuration{}, err
		}
	}
	if err := value.Validate(); err != nil {
		return connectionconfig.Configuration{}, catalogInvalidArgument(err)
	}
	if _, exists := catalog.records[value.ConfigurationID]; exists {
		return connectionconfig.Configuration{}, document.NewFailure(document.FailureConflict, nil)
	}
	if value.AutoLogin {
		for _, existing := range catalog.records {
			if existing.configuration.AutoLogin {
				return connectionconfig.Configuration{}, AutoLoginConflict{}
			}
		}
	}
	candidate := cloneRecords(catalog.records)
	candidate[value.ConfigurationID] = catalogRecord{configuration: value.Clone(), password: password}
	if err := catalog.commit(ctx, candidate, allowUnprotected); err != nil {
		return connectionconfig.Configuration{}, err
	}
	return value.Clone(), nil
}

func (catalog *Catalog) nextConfigurationID() (connectionconfig.ID, error) {
	for range 8 {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return "", fmt.Errorf("generate configuration ID: %w", err)
		}
		candidate := connectionconfig.ID(fmt.Sprintf("cfg-%x", entropy))
		if _, exists := catalog.records[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", document.NewFailure(document.FailureConflict, nil)
}

type Update struct {
	connectionconfig.Patch
	ExpectedVersion      string
	Password             *string
	AllowInsecureStorage bool
}

func (catalog *Catalog) Update(ctx context.Context, id connectionconfig.ID, update Update) (connectionconfig.Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	if !id.Valid() || update.Patch.Empty() && update.Password == nil {
		return connectionconfig.Configuration{}, catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return connectionconfig.Configuration{}, document.NewFailure(document.FailureNotFound, nil)
	}
	if update.ExpectedVersion != "" && record.version != update.ExpectedVersion {
		return connectionconfig.Configuration{}, ErrVersionConflict
	}
	record.configuration = update.Patch.Apply(record.configuration)
	if update.Password != nil {
		record.password = *update.Password
	}
	if err := record.configuration.Validate(); err != nil {
		return connectionconfig.Configuration{}, catalogInvalidArgument(err)
	}
	if record.configuration.AutoLogin {
		for existingID, existing := range catalog.records {
			if existingID != id && existing.configuration.AutoLogin {
				return connectionconfig.Configuration{}, AutoLoginConflict{}
			}
		}
	}
	candidate := cloneRecords(catalog.records)
	candidate[id] = record
	if err := catalog.commit(ctx, candidate, update.AllowInsecureStorage || update.Password == nil && catalog.unprotectedConsent); err != nil {
		return connectionconfig.Configuration{}, err
	}
	return record.configuration.Clone(), nil
}

func (catalog *Catalog) SetPassword(ctx context.Context, id connectionconfig.ID, password string, allowUnprotected bool) (connectionconfig.Configuration, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	if !id.Valid() {
		return connectionconfig.Configuration{}, catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return connectionconfig.Configuration{}, document.NewFailure(document.FailureNotFound, nil)
	}
	record.password = password
	candidate := cloneRecords(catalog.records)
	candidate[id] = record
	if err := catalog.commit(ctx, candidate, allowUnprotected); err != nil {
		return connectionconfig.Configuration{}, err
	}
	return record.configuration.Clone(), nil
}

func (catalog *Catalog) Delete(ctx context.Context, id connectionconfig.ID, expectedVersion string, allow ...bool) error {
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if !id.Valid() {
		return catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	record, exists := catalog.records[id]
	if !exists {
		return document.NewFailure(document.FailureNotFound, nil)
	}
	if record.version != expectedVersion {
		return ErrVersionConflict
	}
	candidate := cloneRecords(catalog.records)
	delete(candidate, id)
	return catalog.commit(ctx, candidate, len(allow) > 0 && allow[0])
}

func (catalog *Catalog) Resolve(ctx context.Context, id connectionconfig.ID) (connectionconfig.Configuration, credential.AuthenticationCredential, error) {
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, credential.AuthenticationCredential{}, err
	}
	if !id.Valid() {
		return connectionconfig.Configuration{}, credential.AuthenticationCredential{}, catalogInvalidArgument(nil)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return connectionconfig.Configuration{}, credential.AuthenticationCredential{}, err
	}
	record, exists := catalog.records[id]
	if !exists {
		return connectionconfig.Configuration{}, credential.AuthenticationCredential{}, document.NewFailure(document.FailureNotFound, nil)
	}
	return record.configuration.Clone(), credential.AuthenticationCredential{Username: record.configuration.Username, Password: record.password}, nil
}

func (catalog *Catalog) StorageProtection() securefile.ProtectionStatus {
	return catalog.store.ProtectionStatus()
}

var ErrVersionConflict = errors.New("configuration version changed")

func (catalog *Catalog) commit(ctx context.Context, candidate map[connectionconfig.ID]catalogRecord, allow bool) error {
	for id, record := range candidate {
		old, exists := catalog.records[id]
		if !exists || !reflect.DeepEqual(old, record) {
			version, err := newConfigurationVersion()
			if err != nil {
				return err
			}
			record.version = version
			candidate[id] = record
		}
	}
	data, err := encodeCatalogDocument(candidate)
	if err != nil {
		return err
	}
	// Reject an oversized candidate before creating even a recovery file.
	if int64(len(data))+1 > catalogFileSizeLimit {
		return document.NewFailure(document.FailureSizeLimitExceeded, nil)
	}
	if len(catalog.migrationOriginal) > 0 {
		if err := catalog.preserveBeforeMigration(ctx, allow); err != nil {
			return err
		}
	}
	// The store applies catalogFileSizeLimit to the exact bytes it persists and
	// re-reads, so a candidate that could not be read back is rejected before any
	// replacement and before the in-memory authority is committed.
	if err := catalog.store.ReplaceSensitive(ctx, catalog.path, data, catalogFileSizeLimit, allow); err != nil {
		return err
	}
	catalog.migrationOriginal = nil
	catalog.records = candidate
	catalog.unprotectedConsent = allow && catalog.store.ProtectionStatus() == securefile.ProtectionUnprotected
	return nil
}

// preserveBeforeMigration uses the same sensitive storage authority and ACL
// policy as the catalog. A directly restorable recovery copy retains the exact source
// bytes (including whitespace) rather than silently normalizing an old file.
// The random private filename is never reused or included in diagnostics.
func (catalog *Catalog) preserveBeforeMigration(ctx context.Context, allow bool) error {
	unchanged := func() error {
		current, exists, err := catalog.store.Read(ctx, catalog.path, catalogFileSizeLimit)
		if err != nil {
			return err
		}
		if !exists || !bytes.Equal(current, catalog.migrationOriginal) {
			return document.NewFailure(document.FailureConflict, nil)
		}
		return nil
	}
	if err := unchanged(); err != nil {
		return err
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return document.NewFailure(document.FailureAtomicWrite, err)
	}
	name := ".sidravia-migration-" + hex.EncodeToString(entropy[:]) + ".json"
	if err := catalog.store.PreserveSensitive(ctx, filepath.Join(filepath.Dir(catalog.path), name), catalog.migrationOriginal, catalogFileSizeLimit, allow); err != nil {
		return err
	}
	if err := unchanged(); err != nil {
		return err
	}
	return nil
}

func cloneRecords(source map[connectionconfig.ID]catalogRecord) map[connectionconfig.ID]catalogRecord {
	result := make(map[connectionconfig.ID]catalogRecord, len(source))
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
	return document.NewFailure(document.FailureInvalidArgument, cause)
}
