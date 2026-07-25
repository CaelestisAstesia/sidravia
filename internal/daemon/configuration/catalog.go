package configuration

import (
	"context"
	"path/filepath"
	"sort"
	"sync"

	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const catalogFileSizeLimit int64 = 1 * 1024 * 1024

type Catalog struct {
	mu             sync.Mutex
	store          jsonfile.Store
	path           string
	configurations map[ConfigurationID]Configuration
}

func OpenCatalog(ctx context.Context, store jsonfile.Store, path string) (*Catalog, error) {
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
	configurations := make(map[ConfigurationID]Configuration)
	if exists {
		configurations, err = decodeCatalogDocument(data)
		if err != nil {
			return nil, err
		}
	}
	return &Catalog{store: store, path: path, configurations: configurations}, nil
}

func (catalog *Catalog) Get(ctx context.Context, id ConfigurationID) (Configuration, error) {
	if id == "" {
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
	configuration, exists := catalog.configurations[id]
	if !exists {
		return Configuration{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return configuration.Clone(), nil
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
	identifiers := make([]ConfigurationID, 0, len(catalog.configurations))
	for identifier := range catalog.configurations {
		identifiers = append(identifiers, identifier)
	}
	sort.Slice(identifiers, func(left, right int) bool { return identifiers[left] < identifiers[right] })
	configurations := make([]Configuration, 0, len(identifiers))
	for _, identifier := range identifiers {
		configurations = append(configurations, catalog.configurations[identifier].Clone())
	}
	return configurations, nil
}

func (catalog *Catalog) Save(ctx context.Context, configuration Configuration) error {
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if err := configuration.Validate(); err != nil {
		return catalogInvalidArgument(err)
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	candidate := cloneConfigurationMap(catalog.configurations)
	candidate[configuration.ConfigurationID] = configuration.Clone()
	data, err := encodeCatalogDocument(candidate)
	if err != nil {
		return err
	}
	if err := catalog.store.Replace(ctx, catalog.path, data); err != nil {
		return err
	}
	catalog.configurations = candidate
	return nil
}

func (catalog *Catalog) Delete(ctx context.Context, id ConfigurationID) error {
	if id == "" {
		return catalogInvalidArgument(nil)
	}
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := validateCatalogContext(ctx); err != nil {
		return err
	}
	if _, exists := catalog.configurations[id]; !exists {
		return persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	candidate := cloneConfigurationMap(catalog.configurations)
	delete(candidate, id)
	data, err := encodeCatalogDocument(candidate)
	if err != nil {
		return err
	}
	if err := catalog.store.Replace(ctx, catalog.path, data); err != nil {
		return err
	}
	catalog.configurations = candidate
	return nil
}

func cloneConfigurationMap(source map[ConfigurationID]Configuration) map[ConfigurationID]Configuration {
	cloned := make(map[ConfigurationID]Configuration, len(source))
	for identifier, configuration := range source {
		cloned[identifier] = configuration.Clone()
	}
	return cloned
}

func validateCatalogContext(ctx context.Context) error {
	if ctx == nil {
		return catalogInvalidArgument(nil)
	}
	if err := ctx.Err(); err != nil {
		return catalogInvalidArgument(err)
	}
	return nil
}

func catalogInvalidArgument(cause error) error {
	return persistence.NewFailure(persistence.FailureInvalidArgument, cause)
}
