package configuration

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sidravia/internal/daemon/persistence/jsonfile"
	"testing"
)

type consentCatalogStore struct {
	catalogMemoryStore
	unsupported bool
	status      jsonfile.ProtectionStatus
}

func (s *consentCatalogStore) ProtectionStatus() jsonfile.ProtectionStatus { return s.status }
func (s *consentCatalogStore) ReplaceSensitive(ctx context.Context, path string, data []byte, maximum int64, allow bool) error {
	if s.unsupported && !allow {
		return jsonfile.ErrInsecureStorageConfirmationRequired
	}
	if err := s.catalogMemoryStore.ReplaceSensitive(ctx, path, data, maximum, allow); err != nil {
		return err
	}
	if s.unsupported {
		s.status = jsonfile.ProtectionUnprotected
	}
	return nil
}
func TestCatalogAtomicCredentialEdits(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "persistence failure"}[failure], func(t *testing.T) {
			store := &catalogMemoryStore{}
			catalog, err := OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			c := catalogTestConfiguration("a", "old")
			if _, err = catalog.Create(ctx, c, "old-password", false); err != nil {
				t.Fatal(err)
			}
			before := append([]byte(nil), store.data...)
			calls := store.replaceCalls
			if failure {
				store.replaceErr = errors.New("write failed")
			}
			username, password, profile := "new-user", "new-password", InstitutionProfileID("new-profile")
			login, reconnect := true, false
			_, err = catalog.Update(ctx, "a", Update{Username: &username, InstitutionProfileID: &profile, Password: &password, AutoLogin: &login, AutoReconnect: &reconnect})
			got, credential, readErr := catalog.Resolve(ctx, "a")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failure {
				if err == nil || !bytes.Equal(store.data, before) || got.Username != c.Username || credential.Password != "old-password" || got.AutoLogin != c.AutoLogin || got.AutoReconnect != c.AutoReconnect || got.InstitutionProfileID != c.InstitutionProfileID {
					t.Fatal("failed edit changed aggregate")
				}
			} else {
				if err != nil || got.Username != username || credential.Password != password || got.InstitutionProfileID != profile || !got.AutoLogin || got.AutoReconnect || store.replaceCalls != calls+1 {
					t.Fatal("edit did not commit once")
				}
				reopened, e := OpenCatalog(ctx, store, catalog.path)
				if e != nil {
					t.Fatal(e)
				}
				_, cred, e := reopened.Resolve(ctx, "a")
				if e != nil || cred.Password != password {
					t.Fatal("password did not persist")
				}
				next := "password-only"
				if _, e = catalog.Update(ctx, "a", Update{Password: &next}); e != nil {
					t.Fatal(e)
				}
				name := "metadata-only"
				if _, e = catalog.Update(ctx, "a", Update{DisplayName: &name}); e != nil {
					t.Fatal(e)
				}
				_, cred, e = catalog.Resolve(ctx, "a")
				if e != nil || cred.Password != next {
					t.Fatal("metadata changed password")
				}
			}
		})
	}
}
func TestCatalogUpdateDeleteConsentAndProtectionFailureAreAtomic(t *testing.T) {
	ctx := context.Background()
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "delete"}[remove], func(t *testing.T) {
			store := &consentCatalogStore{status: jsonfile.ProtectionProtected}
			path := filepath.Join(t.TempDir(), "config.json")
			catalog, err := OpenCatalog(ctx, store, path)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []ConfigurationID{"a", "b"} {
				if _, err = catalog.Create(ctx, catalogTestConfiguration(id, string(id)), "private-password", false); err != nil {
					t.Fatal(err)
				}
			}
			before := append([]byte(nil), store.data...)
			store.unsupported = true
			name, password := "new", "new-private"
			if remove {
				err = catalog.Delete(ctx, "a")
			} else {
				_, err = catalog.Update(ctx, "a", Update{DisplayName: &name, Password: &password})
			}
			if !errors.Is(err, jsonfile.ErrInsecureStorageConfirmationRequired) || !bytes.Equal(before, store.data) || catalog.StorageProtection() != jsonfile.ProtectionProtected {
				t.Fatal("unconsented downgrade changed disk/protection")
			}
			if _, cred, e := catalog.Resolve(ctx, "a"); e != nil || cred.Password != "private-password" {
				t.Fatal("rejection changed memory")
			}
			if remove {
				err = catalog.Delete(ctx, "a", true)
			} else {
				_, err = catalog.Update(ctx, "a", Update{DisplayName: &name, Password: &password, AllowInsecureStorage: true})
			}
			if err != nil || catalog.StorageProtection() != jsonfile.ProtectionUnprotected {
				t.Fatal("explicit consent rejected")
			}
			// Only metadata rewrites reuse current-instance successful consent.
			if _, err = catalog.Update(ctx, "b", Update{DisplayName: &name}); err != nil {
				t.Fatal(err)
			}
			if _, err = catalog.Update(ctx, "b", Update{Password: &password}); !errors.Is(err, jsonfile.ErrInsecureStorageConfirmationRequired) {
				t.Fatal("new password bypassed consent")
			}
			reopened, e := OpenCatalog(ctx, store, path)
			if e != nil {
				t.Fatal(e)
			}
			if _, err = reopened.Update(ctx, "b", Update{DisplayName: &name}); !errors.Is(err, jsonfile.ErrInsecureStorageConfirmationRequired) {
				t.Fatal("reopen invented consent")
			}
		})
	}
}

func TestProtectionRecoveryRevokesEarlierDowngradeConsent(t *testing.T) {
	ctx := context.Background()
	store := &consentCatalogStore{status: jsonfile.ProtectionUnprotected, unsupported: true}
	catalog, err := OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.Create(ctx, catalogTestConfiguration("a", "user"), "password", true); err != nil {
		t.Fatal(err)
	}
	store.unsupported = false
	store.status = jsonfile.ProtectionProtected
	name := "protected-again"
	if _, err = catalog.Update(ctx, "a", Update{DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), store.data...)
	store.unsupported = true
	if err = catalog.Delete(ctx, "a"); !errors.Is(err, jsonfile.ErrInsecureStorageConfirmationRequired) {
		t.Fatal("old consent authorized a new downgrade")
	}
	if !bytes.Equal(before, store.data) {
		t.Fatal("unconsented downgrade changed disk")
	}
	if _, err = catalog.Get(ctx, "a"); err != nil {
		t.Fatal("denial removed config")
	}
}
