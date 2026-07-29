package configuration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type catalogMemoryStore struct {
	data         []byte
	exists       bool
	readErr      error
	replaceErr   error
	readMaximum  int64
	replaceCalls int
}

func (store *catalogMemoryStore) Read(_ context.Context, _ string, maximum int64) ([]byte, bool, error) {
	store.readMaximum = maximum
	return append([]byte(nil), store.data...), store.exists, store.readErr
}

func (store *catalogMemoryStore) Replace(_ context.Context, _ string, data []byte) error {
	return store.ReplaceSensitive(context.Background(), "", data, true)
}

func (store *catalogMemoryStore) ReplaceSensitive(_ context.Context, _ string, data []byte, _ bool) error {
	store.replaceCalls++
	if store.replaceErr != nil {
		return store.replaceErr
	}
	store.data = append([]byte(nil), data...)
	store.exists = true
	return nil
}

func (store *catalogMemoryStore) ProtectionStatus() jsonfile.ProtectionStatus {
	return jsonfile.ProtectionProtected
}

func TestCatalogLifecycleAndRestart(t *testing.T) {
	ctx := context.Background()
	store := &catalogMemoryStore{}
	path := filepath.Join(t.TempDir(), "configurations.json")
	catalog, err := OpenCatalog(ctx, store, path)
	if err != nil {
		t.Fatalf("OpenCatalog() error = %v", err)
	}
	if configurations, err := catalog.List(ctx); err != nil || len(configurations) != 0 {
		t.Fatalf("initial List() = %#v, %v", configurations, err)
	}

	second := catalogTestConfiguration("configuration-b", "Second")
	second.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(`{"server":"b"}`)
	first := catalogTestConfiguration("configuration-a", "First")
	if err := catalog.Create(ctx, second, "second-password", false); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if err := catalog.Create(ctx, first, "", false); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}

	listed, err := catalog.List(ctx)
	if err != nil || len(listed) != 2 || listed[0].ConfigurationID != "configuration-a" || listed[1].ConfigurationID != "configuration-b" {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
	listed[1].ProtocolContextOverride[0] = '['
	got, err := catalog.Get(ctx, "configuration-b")
	if err != nil || string(got.ProtocolContextOverride) != `{"server":"b"}` {
		t.Fatalf("Get(configuration-b) = %#v, %v", got, err)
	}

	second.DisplayName = "Updated"
	if _, err := catalog.Update(ctx, second.ConfigurationID, Update{DisplayName: &second.DisplayName}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := catalog.Delete(ctx, "configuration-a"); err != nil {
		t.Fatalf("Delete(configuration-a) error = %v", err)
	}
	reopened, err := OpenCatalog(ctx, store, path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	listed, err = reopened.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].ConfigurationID != "configuration-b" || listed[0].DisplayName != "Updated" {
		t.Fatalf("reopened List() = %#v, %v", listed, err)
	}
	if store.readMaximum != catalogFileSizeLimit || store.replaceCalls != 4 {
		t.Fatalf("store maximum = %d, replace calls = %d", store.readMaximum, store.replaceCalls)
	}
}

func TestCatalogRejectsInvalidCallsAndReportsMissingIDs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "configurations.json")
	if _, err := OpenCatalog(ctx, nil, path); configurationPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("OpenCatalog(nil store) error = %v", err)
	}
	if _, err := OpenCatalog(ctx, &catalogMemoryStore{}, "relative.json"); configurationPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("OpenCatalog(relative path) error = %v", err)
	}
	catalog, err := OpenCatalog(ctx, &catalogMemoryStore{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Get(ctx, "missing"); configurationPersistenceFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Get(missing) error = %v", err)
	}
	if err := catalog.Delete(ctx, "missing"); configurationPersistenceFailureCode(t, err) != persistence.FailureNotFound {
		t.Fatalf("Delete(missing) error = %v", err)
	}
	if err := catalog.Create(ctx, Configuration{}, "", false); configurationPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("Create(invalid) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := catalog.List(canceled); configurationPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
		t.Fatalf("List(canceled) error = %v", err)
	}
}

func TestCatalogFailedReplacePreservesMemoryState(t *testing.T) {
	ctx := context.Background()
	store := &catalogMemoryStore{}
	catalog, err := OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := catalogTestConfiguration("configuration-1", "Original")
	if err := catalog.Create(ctx, original, "password", false); err != nil {
		t.Fatal(err)
	}
	store.replaceErr = persistence.NewFailure(persistence.FailureAtomicWrite, errors.New("write failed"))
	replacement := "Replacement"
	if _, err := catalog.Update(ctx, original.ConfigurationID, Update{DisplayName: &replacement}); configurationPersistenceFailureCode(t, err) != persistence.FailureAtomicWrite {
		t.Fatalf("Update replacement error = %v", err)
	}
	got, err := catalog.Get(ctx, original.ConfigurationID)
	if err != nil || got.DisplayName != "Original" {
		t.Fatalf("Get() after failed Save = %#v, %v", got, err)
	}
	if err := catalog.Delete(ctx, original.ConfigurationID); configurationPersistenceFailureCode(t, err) != persistence.FailureAtomicWrite {
		t.Fatalf("Delete error = %v", err)
	}
	if _, err := catalog.Get(ctx, original.ConfigurationID); err != nil {
		t.Fatalf("Get() after failed Delete error = %v", err)
	}
}

func TestOpenCatalogPreservesReadFailure(t *testing.T) {
	want := persistence.NewFailure(persistence.FailurePermissionDenied, errors.New("read denied"))
	_, err := OpenCatalog(context.Background(), &catalogMemoryStore{readErr: want}, filepath.Join(t.TempDir(), "configurations.json"))
	if !errors.Is(err, want) {
		t.Fatalf("OpenCatalog() error = %v, want original failure", err)
	}
}

func catalogTestConfiguration(id ConfigurationID, displayName string) Configuration {
	return Configuration{
		ConfigurationID:      id,
		DisplayName:          displayName,
		InstitutionProfileID: InstitutionProfileID("profile-" + id),
		Username:             "user-" + string(id),
		NetworkBindingPolicy: NetworkBindingPolicy{Mode: AutomaticallySelectLatestAvailable},
	}
}
