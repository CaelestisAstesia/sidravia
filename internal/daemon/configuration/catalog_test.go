package configuration

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
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
	if _, err := catalog.Create(ctx, second, "second-password", false); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if _, err := catalog.Create(ctx, first, "", false); err != nil {
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
	if _, err := catalog.Create(ctx, Configuration{}, "", false); configurationPersistenceFailureCode(t, err) != persistence.FailureInvalidArgument {
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
	if _, err := catalog.Create(ctx, original, "password", false); err != nil {
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

func TestCatalogCommitEnforcesReadableSizeLimit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "configurations.json")
	configuration := catalogTestConfiguration("configuration-size-limit", "Size limit")
	candidate := map[ConfigurationID]catalogRecord{
		configuration.ConfigurationID: {configuration: configuration, password: ""},
	}
	emptyData, err := encodeCatalogDocument(candidate)
	if err != nil {
		t.Fatal(err)
	}
	passwordLength := int(catalogFileSizeLimit) - len(emptyData)
	if passwordLength < 0 {
		t.Fatalf("empty encoded catalog size = %d, limit = %d", len(emptyData), catalogFileSizeLimit)
	}
	exactPassword := strings.Repeat("x", passwordLength)
	candidate[configuration.ConfigurationID] = catalogRecord{configuration: configuration, password: exactPassword}
	exactData, err := encodeCatalogDocument(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(exactData) != int(catalogFileSizeLimit) {
		t.Fatalf("exact encoded catalog size = %d, limit = %d", len(exactData), catalogFileSizeLimit)
	}

	store := &catalogMemoryStore{}
	catalog, err := OpenCatalog(ctx, store, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Create(ctx, configuration, exactPassword, false); err != nil {
		t.Fatalf("Create(exact limit) error = %v", err)
	}
	if _, err := OpenCatalog(ctx, store, path); err != nil {
		t.Fatalf("OpenCatalog() after exact-limit create error = %v", err)
	}

	replaceCalls := store.replaceCalls
	if _, err := catalog.SetPassword(ctx, configuration.ConfigurationID, exactPassword+"x", false); configurationPersistenceFailureCode(t, err) != persistence.FailureSizeLimitExceeded {
		t.Fatalf("SetPassword(one byte over) error = %v", err)
	}
	if store.replaceCalls != replaceCalls {
		t.Fatalf("ReplaceSensitive calls after oversized candidate = %d, want %d", store.replaceCalls, replaceCalls)
	}
	_, credential, err := catalog.Resolve(ctx, configuration.ConfigurationID)
	if err != nil || credential.Password != exactPassword {
		t.Fatalf("Resolve() after oversized candidate = %#v, %v", credential, err)
	}
	if _, err := catalog.SetPassword(ctx, configuration.ConfigurationID, exactPassword, false); err != nil {
		t.Fatalf("SetPassword(retry exact limit) error = %v", err)
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

func TestCatalogCreateRejectsSecondAutoLogin(t *testing.T) {
	ctx := context.Background()
	catalog, err := OpenCatalog(ctx, &catalogMemoryStore{}, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	first := catalogTestConfiguration("configuration-a", "First")
	first.AutoLogin = true
	if _, err := catalog.Create(ctx, first, "password", false); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second := catalogTestConfiguration("configuration-b", "Second")
	second.AutoLogin = true
	_, err = catalog.Create(ctx, second, "password", false)
	if _, ok := err.(AutoLoginConflict); !ok {
		t.Fatalf("Create(second) error = %v, want AutoLoginConflict", err)
	}
}

func TestCatalogCreateGeneratesAndPersistsOpaqueIdentity(t *testing.T) {
	ctx := context.Background()
	store := &catalogMemoryStore{}
	path := filepath.Join(t.TempDir(), "configurations.json")
	catalog, err := OpenCatalog(ctx, store, path)
	if err != nil {
		t.Fatal(err)
	}
	first := catalogTestConfiguration("", "First")
	second := catalogTestConfiguration("", "Second")
	first.InstitutionProfileID, second.InstitutionProfileID = "profile-first", "profile-second"
	first.Username, second.Username = "first", "second"
	persistedFirst, err := catalog.Create(ctx, first, "first-password", false)
	if err != nil {
		t.Fatal(err)
	}
	persistedSecond, err := catalog.Create(ctx, second, "second-password", false)
	if err != nil {
		t.Fatal(err)
	}
	format := regexp.MustCompile(`^cfg-[0-9a-f]{32}$`)
	if !format.MatchString(string(persistedFirst.ConfigurationID)) || !format.MatchString(string(persistedSecond.ConfigurationID)) || persistedFirst.ConfigurationID == persistedSecond.ConfigurationID {
		t.Fatalf("generated identities = %q, %q", persistedFirst.ConfigurationID, persistedSecond.ConfigurationID)
	}
	got, credential, err := catalog.Resolve(ctx, persistedFirst.ConfigurationID)
	if err != nil || got.ConfigurationID != persistedFirst.ConfigurationID || credential.Password != "first-password" {
		t.Fatalf("Resolve(generated) = %#v, %#v, %v", got, credential, err)
	}
	reopened, err := OpenCatalog(ctx, store, path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.Get(ctx, persistedSecond.ConfigurationID); err != nil || got.ConfigurationID != persistedSecond.ConfigurationID {
		t.Fatalf("reopened generated Configuration = %#v, %v", got, err)
	}
}

func TestCatalogUpdateRejectsSecondAutoLogin(t *testing.T) {
	ctx := context.Background()
	catalog, err := OpenCatalog(ctx, &catalogMemoryStore{}, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	first := catalogTestConfiguration("configuration-a", "First")
	first.AutoLogin = true
	if _, err := catalog.Create(ctx, first, "password", false); err != nil {
		t.Fatal(err)
	}
	second := catalogTestConfiguration("configuration-b", "Second")
	if _, err := catalog.Create(ctx, second, "password", false); err != nil {
		t.Fatal(err)
	}
	enable := true
	_, err = catalog.Update(ctx, second.ConfigurationID, Update{AutoLogin: &enable})
	if _, ok := err.(AutoLoginConflict); !ok {
		t.Fatalf("Update() error = %v, want AutoLoginConflict", err)
	}
	got, err := catalog.Get(ctx, second.ConfigurationID)
	if err != nil || got.AutoLogin {
		t.Fatalf("Get() after failed update = %#v, %v", got, err)
	}
}

func TestCatalogUpdateAutoLoginAtomicCommit(t *testing.T) {
	ctx := context.Background()
	store := &catalogMemoryStore{}
	catalog, err := OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	first := catalogTestConfiguration("configuration-a", "First")
	first.AutoLogin = true
	if _, err := catalog.Create(ctx, first, "password", false); err != nil {
		t.Fatal(err)
	}
	second := catalogTestConfiguration("configuration-b", "Second")
	if _, err := catalog.Create(ctx, second, "password", false); err != nil {
		t.Fatal(err)
	}
	store.replaceErr = persistence.NewFailure(persistence.FailureAtomicWrite, errors.New("write failed"))
	enable := true
	if _, err := catalog.Update(ctx, second.ConfigurationID, Update{AutoLogin: &enable}); err == nil {
		t.Fatal("Update() unexpectedly succeeded")
	}
	got, err := catalog.Get(ctx, first.ConfigurationID)
	if err != nil || !got.AutoLogin {
		t.Fatalf("first configuration after failed update = %#v, %v", got, err)
	}
	gotSecond, err := catalog.Get(ctx, second.ConfigurationID)
	if err != nil || gotSecond.AutoLogin {
		t.Fatalf("second configuration after failed update = %#v, %v", gotSecond, err)
	}
}
