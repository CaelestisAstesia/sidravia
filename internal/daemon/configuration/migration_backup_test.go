package configuration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sidravia/internal/persistence"
	"sidravia/internal/persistence/jsonfile"
)

func legacyCatalogFixture() []byte {
	return []byte(" \n" + `{"schemaVersion":2,"configurations":[{"configurationId":"configuration-1","displayName":"Campus","institutionProfileId":"profile-1","username":"synthetic-user","password":"synthetic-password","networkBindingPolicy":{"mode":"automatically_select_latest_available"},"protocolContextOverride":null}]}` + "\n")
}

func TestCatalogMigrationPreservesExactOldDocumentBeforeFirstWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configurations.json")
	original := legacyCatalogFixture()
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := OpenCatalog(context.Background(), store, path)
	if err != nil {
		t.Fatal(err)
	}
	assertBackups := func(want int) []string {
		t.Helper()
		paths, err := filepath.Glob(filepath.Join(root, ".sidravia-migration-*.json"))
		if err != nil || len(paths) != want {
			t.Fatalf("backup count %d, want %d: %v", len(paths), want, err)
		}
		return paths
	}
	assertBackups(0)
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
		t.Fatal("read changed old bytes")
	}
	name := "Updated"
	if _, err := catalog.Update(context.Background(), "configuration-1", Update{DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(assertBackups(1)[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, original) {
		t.Fatal("recovery copy is not directly restorable")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(`"schemaVersion":4`)) || !bytes.Contains(data, []byte(`"password":"synthetic-password"`)) {
		t.Fatal("migration lost configuration")
	}
	if _, err := catalog.Update(context.Background(), "configuration-1", Update{DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	assertBackups(1)
}

func TestCatalogBackupFailureLeavesMemoryAndOriginalDocumentUnchanged(t *testing.T) {
	store := &catalogMemoryStore{data: legacyCatalogFixture(), exists: true}
	catalog, err := OpenCatalog(context.Background(), store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), store.data...)
	store.replaceErr = persistence.NewFailure(persistence.FailureAtomicWrite, errors.New("synthetic backup failure"))
	name := "Updated"
	if _, err := catalog.Update(context.Background(), "configuration-1", Update{DisplayName: &name}); err == nil {
		t.Fatal("write continued without a recovery copy")
	}
	got, err := catalog.Get(context.Background(), "configuration-1")
	if err != nil || got.DisplayName != "Campus" || !bytes.Equal(before, store.data) {
		t.Fatal("backup failure committed a candidate")
	}
}

func TestCatalogMigrationRejectsChangedSourceBeforeWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configurations.json")
	original := legacyCatalogFixture()
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := OpenCatalog(context.Background(), store, path)
	if err != nil {
		t.Fatal(err)
	}
	external := bytes.ReplaceAll(original, []byte("Campus"), []byte("External"))
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	name := "Updated"
	if _, err := catalog.Update(context.Background(), "configuration-1", Update{DisplayName: &name}); configurationPersistenceFailureCode(t, err) != persistence.FailureConflict {
		t.Fatalf("changed source not rejected: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, external) {
		t.Fatal("external source was overwritten")
	}
	backups, err := filepath.Glob(filepath.Join(root, ".sidravia-migration-*.json"))
	if err != nil || len(backups) != 0 {
		t.Fatal("wrote backup after source conflict")
	}
}

type failedMigrationStore struct {
	*jsonfile.SecureStore
	fail bool
}

func (s *failedMigrationStore) ReplaceSensitive(ctx context.Context, path string, data []byte, limit int64, allow bool) error {
	if s.fail {
		return persistence.NewFailure(persistence.FailureAtomicWrite, errors.New("synthetic migration commit failure"))
	}
	return s.SecureStore.ReplaceSensitive(ctx, path, data, limit, allow)
}

func TestFailedMigrationRetryStillChecksOriginal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configurations.json")
	original := legacyCatalogFixture()
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	secure, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	store := &failedMigrationStore{SecureStore: secure, fail: true}
	catalog, err := OpenCatalog(context.Background(), store, path)
	if err != nil {
		t.Fatal(err)
	}
	name := "Candidate"
	if _, err := catalog.Update(context.Background(), "configuration-1", Update{DisplayName: &name}); err == nil {
		t.Fatal("failed commit accepted")
	}
	changed := bytes.Replace(original, []byte("Campus"), []byte("External"), 1)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	store.fail = false
	if _, err := catalog.Update(context.Background(), "configuration-1", Update{DisplayName: &name}); err == nil {
		t.Fatal("retry overwrote a changed source")
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, changed) {
		t.Fatal("external edit lost")
	}
}
