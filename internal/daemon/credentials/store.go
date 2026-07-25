package credentials

import (
	"context"
	"path/filepath"
	"sync"

	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

const storeFileSizeLimit int64 = 1 * 1024 * 1024

type Store struct {
	mu          sync.Mutex
	storage     jsonfile.Store
	path        string
	credentials map[CredentialID]AuthenticationCredential
}

func OpenStore(ctx context.Context, storage jsonfile.Store, path string) (*Store, error) {
	if storage == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, storeInvalidArgument(nil)
	}
	if err := validateStoreContext(ctx); err != nil {
		return nil, err
	}
	data, exists, err := storage.Read(ctx, path, storeFileSizeLimit)
	if err != nil {
		return nil, err
	}
	credentials := make(map[CredentialID]AuthenticationCredential)
	if exists {
		credentials, err = decodeStoreDocument(data)
		if err != nil {
			return nil, err
		}
	}
	return &Store{storage: storage, path: path, credentials: credentials}, nil
}

func (store *Store) Get(ctx context.Context, id CredentialID) (AuthenticationCredential, error) {
	if id == "" {
		return AuthenticationCredential{}, storeInvalidArgument(nil)
	}
	if err := validateStoreContext(ctx); err != nil {
		return AuthenticationCredential{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateStoreContext(ctx); err != nil {
		return AuthenticationCredential{}, err
	}
	credential, exists := store.credentials[id]
	if !exists {
		return AuthenticationCredential{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return credential, nil
}

func (store *Store) Put(ctx context.Context, id CredentialID, value AuthenticationCredential) error {
	if err := validateCredentialInput(ctx, id, value); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateStoreContext(ctx); err != nil {
		return err
	}
	if _, exists := store.credentials[id]; exists {
		return persistence.NewFailure(persistence.FailureConflict, nil)
	}
	return store.commitWith(ctx, id, value, false)
}

func (store *Store) Replace(ctx context.Context, id CredentialID, value AuthenticationCredential) error {
	if err := validateCredentialInput(ctx, id, value); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateStoreContext(ctx); err != nil {
		return err
	}
	if _, exists := store.credentials[id]; !exists {
		return persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return store.commitWith(ctx, id, value, false)
}

func (store *Store) Delete(ctx context.Context, id CredentialID) error {
	if id == "" {
		return storeInvalidArgument(nil)
	}
	if err := validateStoreContext(ctx); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateStoreContext(ctx); err != nil {
		return err
	}
	if _, exists := store.credentials[id]; !exists {
		return persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return store.commitWith(ctx, id, AuthenticationCredential{}, true)
}

func (store *Store) commitWith(ctx context.Context, id CredentialID, value AuthenticationCredential, deleting bool) error {
	candidate := cloneCredentials(store.credentials)
	if deleting {
		delete(candidate, id)
	} else {
		candidate[id] = value
	}
	data, err := encodeStoreDocument(candidate)
	if err != nil {
		return err
	}
	if err := store.storage.Replace(ctx, store.path, data); err != nil {
		return err
	}
	store.credentials = candidate
	return nil
}

func cloneCredentials(source map[CredentialID]AuthenticationCredential) map[CredentialID]AuthenticationCredential {
	cloned := make(map[CredentialID]AuthenticationCredential, len(source))
	for identifier, credential := range source {
		cloned[identifier] = credential
	}
	return cloned
}

func validateCredentialInput(ctx context.Context, id CredentialID, value AuthenticationCredential) error {
	if id == "" || value.Username == "" {
		return storeInvalidArgument(nil)
	}
	return validateStoreContext(ctx)
}

func validateStoreContext(ctx context.Context) error {
	if ctx == nil {
		return storeInvalidArgument(nil)
	}
	if err := ctx.Err(); err != nil {
		return storeInvalidArgument(err)
	}
	return nil
}

func storeInvalidArgument(cause error) error {
	return persistence.NewFailure(persistence.FailureInvalidArgument, cause)
}
