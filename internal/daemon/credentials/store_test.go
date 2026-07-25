package credentials

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/daemon/persistence"
)

type credentialsMemoryStore struct {
	data         []byte
	exists       bool
	readErr      error
	replaceErr   error
	readMaximum  int64
	replaceCalls int
}

func (storage *credentialsMemoryStore) Read(_ context.Context, _ string, maximum int64) ([]byte, bool, error) {
	storage.readMaximum = maximum
	return append([]byte(nil), storage.data...), storage.exists, storage.readErr
}

func (storage *credentialsMemoryStore) Replace(_ context.Context, _ string, data []byte) error {
	storage.replaceCalls++
	if storage.replaceErr != nil {
		return storage.replaceErr
	}
	storage.data = append([]byte(nil), data...)
	storage.exists = true
	return nil
}

func TestStoreLifecycleAndRestart(t *testing.T) {
	ctx := context.Background()
	storage := &credentialsMemoryStore{}
	path := filepath.Join(t.TempDir(), "credentials.json")
	store, err := OpenStore(ctx, storage, path)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	value := AuthenticationCredential{Username: "private-account", Password: "private-secret"}
	if err := store.Put(ctx, "credential-1", value); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Put(ctx, "credential-1", value); credentialsPersistenceFailureCode(t, err) != persistence.FailureConflict {
		t.Fatalf("duplicate Put() error = %v", err)
	}
	got, err := store.Get(ctx, "credential-1")
	if err != nil || got != value {
		t.Fatalf("Get() = %#v, %v", got, err)
	}
	replacement := AuthenticationCredential{Username: "updated-account", Password: ""}
	if err := store.Replace(ctx, "credential-1", replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	reopened, err := OpenStore(ctx, storage, path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	got, err = reopened.Get(ctx, "credential-1")
	if err != nil || got != replacement {
		t.Fatalf("reopened Get() = %#v, %v", got, err)
	}
	if err := reopened.Delete(ctx, "credential-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := reopened.Get(ctx, "credential-1"); credentialsPersistenceFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Get() after Delete error = %v", err)
	}
	if storage.readMaximum != storeFileSizeLimit || storage.replaceCalls != 3 {
		t.Fatalf("read maximum = %d, replace calls = %d", storage.readMaximum, storage.replaceCalls)
	}
}

func TestStoreRejectsInvalidCallsAndMissingMutations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "credentials.json")
	if _, err := OpenStore(ctx, nil, path); credentialsPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("OpenStore(nil) error = %v", err)
	}
	if _, err := OpenStore(ctx, &credentialsMemoryStore{}, "relative.json"); credentialsPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("OpenStore(relative) error = %v", err)
	}
	store, err := OpenStore(ctx, &credentialsMemoryStore{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "", AuthenticationCredential{Username: "private-account"}); credentialsPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Put(empty id) error = %v", err)
	}
	if err := store.Put(ctx, "credential-private", AuthenticationCredential{}); credentialsPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Put(empty username) error = %v", err)
	}
	if err := store.Replace(ctx, "missing", AuthenticationCredential{Username: "private-account"}); credentialsPersistenceFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Replace(missing) error = %v", err)
	}
	if err := store.Delete(ctx, "missing"); credentialsPersistenceFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Delete(missing) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Get(canceled, "credential-private"); credentialsPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Get(canceled) error = %v", err)
	}
}

func TestStoreFailedReplacePreservesMemoryAndPublicErrorSecrecy(t *testing.T) {
	ctx := context.Background()
	storage := &credentialsMemoryStore{}
	store, err := OpenStore(ctx, storage, filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := AuthenticationCredential{Username: "original-account", Password: "original-secret"}
	if err := store.Put(ctx, "credential-private", original); err != nil {
		t.Fatal(err)
	}
	storage.replaceErr = persistence.NewFailure(persistence.FailureAtomicWrite, errors.New("platform write denied"))
	replacement := AuthenticationCredential{Username: "replacement-account", Password: "replacement-secret"}
	if err := store.Replace(ctx, "credential-private", replacement); credentialsPersistenceFailureCode(t, err) != persistence.FailureAtomicWrite {
		t.Fatalf("Replace failure = %v", err)
	}
	got, err := store.Get(ctx, "credential-private")
	if err != nil || got != original {
		t.Fatalf("Get() after failed Replace did not preserve original")
	}
	if err := store.Delete(ctx, "credential-private"); credentialsPersistenceFailureCode(t, err) != persistence.FailureAtomicWrite {
		t.Fatalf("Delete failure = %v", err)
	}
	for _, secret := range []string{"credential-private", "original-account", "original-secret", "replacement-account", "replacement-secret", "platform write denied"} {
		if strings.Contains(storage.replaceErr.Error(), secret) {
			t.Fatalf("public persistence error leaked secret category %q", secret)
		}
	}
}

func TestOpenStorePreservesReadFailure(t *testing.T) {
	want := persistence.NewFailure(persistence.FailurePermissionDenied, errors.New("platform read denied"))
	_, err := OpenStore(context.Background(), &credentialsMemoryStore{readErr: want}, filepath.Join(t.TempDir(), "credentials.json"))
	if !errors.Is(err, want) {
		t.Fatalf("OpenStore() error = %v, want original failure", err)
	}
}
