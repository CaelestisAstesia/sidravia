package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/environment"
)

func TestApplicationRemovePersistedSessionClearsTrackingButKeepsConfiguration(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	id, _, err := setup.application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.StopSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, id, session.Suspended)
	if err := setup.application.RemoveSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	setup.application.mu.Lock()
	tracked := append([]session.AuthenticationSessionID(nil), setup.application.sessionsByConfig["configuration-1"]...)
	setup.application.mu.Unlock()
	if len(tracked) != 0 {
		t.Fatalf("persisted Session remained tracked: %v", tracked)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err != nil {
		t.Fatalf("Configuration removed with Session: %v", err)
	}
}

func TestApplicationRemoveOneShotDoesNotCreateConfigurationTracking(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	id, _, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.application.RemoveSession(ctx, id); err != nil {
		t.Fatal(err)
	}
	setup.application.mu.Lock()
	tracked := len(setup.application.sessionsByConfig)
	setup.application.mu.Unlock()
	if tracked != 0 {
		t.Fatalf("one-shot removal changed Configuration tracking: %d entries", tracked)
	}
}

// --- Supervisor test deps ---

type appUnavailableRetryPolicy struct{}

func (appUnavailableRetryPolicy) Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool) {
	return 0, false
}

type appNoOpRetryScheduler struct{}

func (appNoOpRetryScheduler) Schedule(time.Duration, func()) session.RetryCancellation {
	return appNoOpRetryCancellation{}
}

type appNoOpRetryCancellation struct{}

func (appNoOpRetryCancellation) Cancel() {}

func appSupervisorDeps() supervisor.Dependencies {
	return supervisor.Dependencies{
		Now:            func() time.Time { return time.Unix(100, 0) },
		RetryPolicy:    appUnavailableRetryPolicy{},
		RetryScheduler: appNoOpRetryScheduler{},
	}
}

// --- toggleableStore ---

type toggleableStore struct {
	data    map[string][]byte
	failing atomic.Bool
}

func (store *toggleableStore) Read(_ context.Context, path string, _ int64) ([]byte, bool, error) {
	data, exists := store.data[path]
	return append([]byte(nil), data...), exists, nil
}

func (store *toggleableStore) Replace(_ context.Context, path string, data []byte) error {
	if store.failing.Load() {
		return errors.New("replace fails")
	}
	store.data[path] = append([]byte(nil), data...)
	return nil
}

// --- Test setup ---

type applicationTestSetup struct {
	application            *Application
	catalog                *config.Catalog
	credStore              *credential.Store
	profileCat             *config.ProfileCatalog
	registry               *protocol.AuthenticationProtocolRegistry
	authenticationResolver *AuthenticationResolver
	supervisor             *supervisor.Supervisor
}

func newApplicationTestSetup(t *testing.T) *applicationTestSetup {
	t.Helper()
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	credStore, err := credential.OpenStore(ctx, store, filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	profileCat, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	if err != nil {
		t.Fatal(err)
	}
	factory := &appTestProtocolFactory{id: "drcom"}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	authenticationResolver, err := NewAuthenticationResolver(catalog, profileCat, credStore, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	supervisorInstance := supervisor.New(appSupervisorDeps())
	application, err := NewApplication(catalog, profileCat, authenticationResolver, supervisorInstance)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Save(ctx, appTestConfiguration("configuration-1")); err != nil {
		t.Fatal(err)
	}
	if err := credStore.Put(ctx, "credential-1", credential.AuthenticationCredential{Username: "user", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	return &applicationTestSetup{
		application:            application,
		catalog:                catalog,
		credStore:              credStore,
		profileCat:             profileCat,
		registry:               registry,
		authenticationResolver: authenticationResolver,
		supervisor:             supervisorInstance,
	}
}

func (setup *applicationTestSetup) cleanup() {
	_ = setup.supervisor.Close()
	setup.supervisor.Wait()
}

// --- Input validation tests ---

func TestBackendRejectsEmptyConfigurationID(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	err := setup.application.DeleteConfiguration(ctx, "")
	if code := deletionFailureCode(t, err); code != DeletionInvalidArgument {
		t.Fatalf("DeleteConfiguration(empty) code = %v, want %v", code, DeletionInvalidArgument)
	}
}

func TestBackendRejectsNilContext(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	err := setup.application.DeleteConfiguration(nil, "config-1")
	if code := deletionFailureCode(t, err); code != DeletionInvalidArgument {
		t.Fatalf("DeleteConfiguration(nil ctx) code = %v, want %v", code, DeletionInvalidArgument)
	}
}

func TestNewBackendRejectsNilDeps(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	credStore, err := credential.OpenStore(ctx, store, filepath.Join(t.TempDir(), "cred.json"))
	if err != nil {
		t.Fatal(err)
	}
	profileCat, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("p1")})
	if err != nil {
		t.Fatal(err)
	}
	factory := &appTestProtocolFactory{id: "drcom"}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	authenticationResolver, err := NewAuthenticationResolver(catalog, profileCat, credStore, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	supervisorInstance := supervisor.New(appSupervisorDeps())
	_ = supervisorInstance.Close()
	supervisorInstance.Wait()

	if _, err := NewApplication(nil, profileCat, authenticationResolver, supervisorInstance); err == nil {
		t.Fatal("NewApplication(nil catalog) error = nil, want error")
	}
	if _, err := NewApplication(catalog, nil, authenticationResolver, supervisorInstance); err == nil {
		t.Fatal("NewApplication(nil profile catalog) error = nil, want error")
	}
	if _, err := NewApplication(catalog, profileCat, nil, supervisorInstance); err == nil {
		t.Fatal("NewApplication(nil authenticationResolver) error = nil, want error")
	}
	if _, err := NewApplication(catalog, profileCat, authenticationResolver, nil); err == nil {
		t.Fatal("NewApplication(nil supervisor) error = nil, want error")
	}
}

func TestApplicationListsRetainedSessionsAndProfiles(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	sessionID, _, err := setup.application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("StartAuthentication() error = %v", err)
	}
	if _, err := setup.application.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	waitForApplicationSessionState(t, setup.application, sessionID, session.Suspended)

	sessions, err := setup.application.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].AuthenticationSessionID != sessionID {
		t.Fatalf("ListSessions() = %#v, want retained %q", sessions, sessionID)
	}
	if sessions[0].State != session.Suspended {
		t.Errorf("retained Session state = %q, want suspended", sessions[0].State)
	}

	profiles, err := setup.application.ListInstitutionProfiles(ctx)
	if err != nil {
		t.Fatalf("ListInstitutionProfiles() error = %v", err)
	}
	if len(profiles) != 1 ||
		profiles[0].InstitutionProfileID != "profile-1" ||
		profiles[0].DisplayName != "profile-1 display" ||
		profiles[0].AuthenticationProtocolID != "drcom" {
		t.Fatalf("ListInstitutionProfiles() = %#v", profiles)
	}
}

// --- Deletion rule tests ---

func TestBackendRejectsDeleteActiveConfiguration(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	sessionID, _, err := setup.application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("StartAuthentication() error = %v", err)
	}
	err = setup.application.DeleteConfiguration(ctx, "configuration-1")
	if code := deletionFailureCode(t, err); code != DeletionConfigurationInUse {
		t.Fatalf("DeleteConfiguration(active) code = %v, want %v", code, DeletionConfigurationInUse)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err != nil {
		t.Fatalf("configuration was deleted despite active reference: %v", err)
	}
	_ = sessionID
}

func TestBackendDeletesConfigurationWithStoppedSession(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	sessionID, _, err := setup.application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("StartAuthentication() error = %v", err)
	}
	if _, err := setup.application.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	waitForApplicationSessionState(t, setup.application, sessionID, session.Suspended)
	if err := setup.application.DeleteConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatalf("DeleteConfiguration(stopped) error = %v", err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err == nil {
		t.Fatal("configuration still exists after deletion")
	}
}

func TestBackendDeletesConfigurationWithNoReferences(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	if err := setup.application.DeleteConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatalf("DeleteConfiguration(no refs) error = %v", err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err == nil {
		t.Fatal("configuration still exists after deletion")
	}
}

func TestBackendDeleteConfigurationNotFound(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	err := setup.application.DeleteConfiguration(ctx, "missing")
	if code := deletionFailureCode(t, err); code != DeletionConfigurationNotFound {
		t.Fatalf("DeleteConfiguration(missing) code = %v, want %v", code, DeletionConfigurationNotFound)
	}
}

// TestBackendCatalogDeleteFailureIsRetryable verifies the forget-first
// deletion order: when catalog persistence fails, sessions are already
// forgotten and tracked entries removed. The same Application can retry
// successfully after the store is recovered.
func TestBackendCatalogDeleteFailureIsRetryable(t *testing.T) {
	ctx := context.Background()
	store := &toggleableStore{data: make(map[string][]byte)}

	catalogPath := filepath.Join(t.TempDir(), "configurations.json")
	catalog, err := config.OpenCatalog(ctx, store, catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	// Save config while store is not failing.
	if err := catalog.Save(ctx, appTestConfiguration("configuration-1")); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(t.TempDir(), "credentials.json")
	credStore, err := credential.OpenStore(ctx, store, credPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := credStore.Put(ctx, "credential-1", credential.AuthenticationCredential{Username: "user", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	profileCat, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	if err != nil {
		t.Fatal(err)
	}
	factory := &appTestProtocolFactory{id: "drcom"}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	authenticationResolver, err := NewAuthenticationResolver(catalog, profileCat, credStore, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(appSupervisorDeps())
	defer func() {
		_ = sup.Close()
		sup.Wait()
	}()
	application, err := NewApplication(catalog, profileCat, authenticationResolver, sup)
	if err != nil {
		t.Fatal(err)
	}

	sessionID, _, err := application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("StartAuthentication() error = %v", err)
	}
	if _, err := application.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	waitForApplicationSessionState(t, application, sessionID, session.Suspended)

	// Now set the store to failing mode for the catalog delete.
	store.failing.Store(true)

	// First deletion: catalog fails, sessions are already forgotten.
	err = application.DeleteConfiguration(ctx, "configuration-1")
	if err == nil {
		t.Fatal("DeleteConfiguration() error = nil, want error")
	}
	if code := deletionFailureCode(t, err); code != DeletionCatalogUnavailable {
		t.Fatalf("DeleteConfiguration(catalog fail) code = %v, want %v", code, DeletionCatalogUnavailable)
	}

	// Verify session is forgotten from Supervisor.
	_, err = sup.Get(ctx, sessionID)
	if err == nil {
		t.Fatal("session should be forgotten from supervisor after failed delete")
	}

	// Verify configuration still exists in catalog.
	_, err = catalog.Get(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("configuration should still exist in catalog after failed delete: %v", err)
	}

	// Recover the store and retry on the same Application.
	store.failing.Store(false)
	if err := application.DeleteConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatalf("retry DeleteConfiguration() error = %v", err)
	}

	// Verify catalog is now empty.
	if _, err := catalog.Get(ctx, "configuration-1"); err == nil {
		t.Fatal("configuration still exists after successful retry")
	}
}

// TestBackendStartDeleteConcurrency verifies that opMu serialization
// prevents StartAuthentication and DeleteConfiguration from interleaving
// in a way that results in an active session referencing a deleted config.
func TestBackendStartDeleteConcurrency(t *testing.T) {
	for round := 0; round < 50; round++ {
		store := &toggleableStore{data: make(map[string][]byte)}
		catalogPath := filepath.Join(t.TempDir(), "configurations.json")
		catalog, err := config.OpenCatalog(context.Background(), store, catalogPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := catalog.Save(context.Background(), appTestConfiguration("configuration-1")); err != nil {
			t.Fatal(err)
		}
		credPath := filepath.Join(t.TempDir(), "credentials.json")
		credStore, err := credential.OpenStore(context.Background(), store, credPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := credStore.Put(context.Background(), "credential-1", credential.AuthenticationCredential{Username: "user", Password: "secret"}); err != nil {
			t.Fatal(err)
		}
		profileCat, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
		if err != nil {
			t.Fatal(err)
		}
		factory := &appTestProtocolFactory{id: "drcom"}
		registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
		if err != nil {
			t.Fatal(err)
		}
		authenticationResolver, err := NewAuthenticationResolver(catalog, profileCat, credStore, registry, appTestHostInfo())
		if err != nil {
			t.Fatal(err)
		}
		sup := supervisor.New(appSupervisorDeps())
		application, err := NewApplication(catalog, profileCat, authenticationResolver, sup)
		if err != nil {
			t.Fatal(err)
		}

		// Start a session first, then stop it so it is eligible for deletion.
		ctx := context.Background()
		sessionID, _, err := application.StartAuthentication(ctx, "configuration-1")
		if err != nil {
			sup.Close()
			sup.Wait()
			t.Fatalf("round %d: StartAuthentication error: %v", round, err)
		}
		if _, err := application.StopSession(ctx, sessionID); err != nil {
			sup.Close()
			sup.Wait()
			t.Fatalf("round %d: StopSession error: %v", round, err)
		}
		waitForApplicationSessionState(t, application, sessionID, session.Suspended)

		// Concurrently start a new session and delete the config.
		done := make(chan struct{}, 2)
		var startErr error
		var deleteErr error

		go func() {
			defer func() { done <- struct{}{} }()
			_, _, startErr = application.StartAuthentication(ctx, "configuration-1")
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			deleteErr = application.DeleteConfiguration(ctx, "configuration-1")
		}()

		<-done
		<-done

		// Verify consistency: no active session referencing deleted config.
		snapshots, listErr := sup.List(ctx)
		if listErr != nil {
			sup.Close()
			sup.Wait()
			t.Fatalf("round %d: List error: %v", round, listErr)
		}

		for _, snap := range snapshots {
			if snap.State != session.Suspended {
				// An active session exists. Verify config still exists.
				_, getErr := catalog.Get(ctx, "configuration-1")
				if getErr != nil {
					t.Errorf("round %d: active session %q references deleted config %q",
						round, snap.AuthenticationSessionID, "configuration-1")
				}
			}
		}

		// If delete succeeded, start must have failed; if start succeeded, delete must have failed.
		if deleteErr == nil && startErr == nil {
			t.Errorf("round %d: both StartAuthentication and DeleteConfiguration succeeded", round)
		}

		sup.Close()
		sup.Wait()
	}
}

// --- Vertical test ---

func TestBackendVerticalStartStopReadSnapshot(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	sessionID, snapshot, err := setup.application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("StartAuthentication() error = %v", err)
	}
	if sessionID == "" {
		t.Fatal("StartAuthentication() returned empty sessionID")
	}
	if snapshot.AuthenticationSessionID != sessionID {
		t.Fatalf("snapshot AuthenticationSessionID = %q, want %q", snapshot.AuthenticationSessionID, sessionID)
	}

	snapshot, err = setup.application.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if snapshot.AuthenticationSessionID != sessionID {
		t.Fatalf("GetSession snapshot AuthenticationSessionID = %q, want %q", snapshot.AuthenticationSessionID, sessionID)
	}

	snapshot, err = setup.application.StopSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if snapshot.State != session.Stopping {
		t.Fatalf("StopSession snapshot State = %q, want %q", snapshot.State, session.Stopping)
	}

	waitForApplicationSessionState(t, setup.application, sessionID, session.Suspended)
}

func deletionFailureCode(t *testing.T, err error) DeletionFailureCode {
	t.Helper()
	var failure *DeletionFailure
	if errors.As(err, &failure) {
		return failure.Code()
	}
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	t.Fatalf("error is not a DeletionFailure: %v", err)
	return ""
}

// --- One-shot authentication tests ---

func TestBackendStartOneShotAuthentication(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	sessionID, snapshot, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatalf("StartOneShotAuthentication() error = %v", err)
	}
	if sessionID == "" {
		t.Fatal("StartOneShotAuthentication() returned empty sessionID")
	}
	if snapshot.AuthenticationSessionID != sessionID {
		t.Fatalf("snapshot AuthenticationSessionID = %q, want %q", snapshot.AuthenticationSessionID, sessionID)
	}

	got, err := setup.application.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.AuthenticationSessionID != sessionID {
		t.Fatalf("GetSession snapshot AuthenticationSessionID = %q, want %q", got.AuthenticationSessionID, sessionID)
	}

	stopped, err := setup.application.StopSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if stopped.State != session.Stopping {
		t.Fatalf("StopSession snapshot State = %q, want %q", stopped.State, session.Stopping)
	}
	waitForApplicationSessionState(t, setup.application, sessionID, session.Suspended)
}

func TestBackendOneShotDoesNotPopulateConfigurationOwnership(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	if _, _, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput()); err != nil {
		t.Fatalf("StartOneShotAuthentication() error = %v", err)
	}

	// No new configuration was created for the one-shot start.
	configs, err := setup.catalog.List(ctx)
	if err != nil {
		t.Fatalf("catalog.List() error = %v", err)
	}
	if len(configs) != 1 || configs[0].ConfigurationID != "configuration-1" {
		t.Fatalf("one-shot start altered the configuration catalog: %v", configs)
	}

	// The active one-shot session owns no configuration tracking entry, so it
	// does not block deletion of the persisted configuration.
	if err := setup.application.DeleteConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatalf("DeleteConfiguration() with active one-shot error = %v", err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err == nil {
		t.Fatal("configuration still exists after deletion")
	}
}

func TestBackendOneShotSingleActiveAdmission(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	// An active one-shot session blocks a second one-shot start.
	first, _, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatalf("first StartOneShotAuthentication() error = %v", err)
	}
	if _, _, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput()); err == nil {
		t.Fatal("second one-shot start succeeded; want single-active rejection")
	}
	// An active one-shot session also blocks a persisted start.
	if _, _, err := setup.application.StartAuthentication(ctx, "configuration-1"); err == nil {
		t.Fatal("persisted start succeeded while one-shot active; want single-active rejection")
	}

	// After the one-shot stops, a persisted start succeeds and then blocks a
	// one-shot start.
	if _, err := setup.application.StopSession(ctx, first); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	waitForApplicationSessionState(t, setup.application, first, session.Suspended)
	persisted, _, err := setup.application.StartAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("persisted start after one-shot stop error = %v", err)
	}
	if _, _, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput()); err == nil {
		t.Fatal("one-shot start succeeded while persisted active; want single-active rejection")
	}
	if _, err := setup.application.StopSession(ctx, persisted); err != nil {
		t.Fatalf("StopSession(persisted) error = %v", err)
	}
}

func TestBackendOneShotFailedStartLeavesSupervisorUsable(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	// A resolution failure creates no session and leaves the Supervisor usable.
	bad := validOneShotInput()
	bad.AuthenticationCredential.Username = ""
	if _, _, err := setup.application.StartOneShotAuthentication(ctx, bad); err == nil {
		t.Fatal("invalid one-shot start succeeded; want error")
	}

	sessionID, _, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatalf("valid one-shot after failed start error = %v", err)
	}
	if sessionID == "" {
		t.Fatal("valid one-shot after failed start returned empty sessionID")
	}
	if _, err := setup.application.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
}

func TestBackendOneShotDoesNotLeakPassword(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	const password = "application-one-shot-password-secret"
	input := validOneShotInput()
	input.AuthenticationCredential.Password = password

	sessionID, snapshot, err := setup.application.StartOneShotAuthentication(ctx, input)
	if err != nil {
		t.Fatalf("StartOneShotAuthentication() error = %v", err)
	}
	assertSnapshotDoesNotContain(t, snapshot, password)
	got, err := setup.application.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	assertSnapshotDoesNotContain(t, got, password)

	if _, err := setup.application.StopSession(ctx, sessionID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}

	// A failing one-shot start must not leak the password in its error.
	failing := validOneShotInput()
	failing.AuthenticationCredential.Password = password
	failing.InstitutionProfileID = "missing-profile"
	_, _, err = setup.application.StartOneShotAuthentication(ctx, failing)
	if err == nil {
		t.Fatal("expected one-shot failure for missing profile")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("returned error leaks password: %v", err)
	}
}

func assertSnapshotDoesNotContain(t *testing.T, snapshot session.Snapshot, secret string) {
	t.Helper()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot) error = %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("public Snapshot exposes secret: %s", encoded)
	}
}

// --- Network snapshot delegation tests ---

// appTestNetworkSnapshot builds a usable environment snapshot for Application
// tests. A usable snapshot carries one operational interface with a valid IPv4
// address, which lets a Session select a binding and enter the authenticating
// state. The revision controls both the snapshot revision and its observedAt
// timestamp.
func appTestNetworkSnapshot(t *testing.T, revision uint64) environment.Snapshot {
	t.Helper()
	iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:              environment.InterfaceID("iface-1"),
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address:      netip.MustParseAddr("192.0.2.10"),
			PrefixLength: 24,
		}},
	})
	if err != nil {
		t.Fatalf("NewNetworkInterface: %v", err)
	}
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{iface})
}

// TestApplicationApplySystemNetworkSnapshotDelegatesToSupervisor proves that
// ApplySystemNetworkSnapshot delegates into the same Supervisor used by
// one-shot start. Without a snapshot a one-shot start waits for network; after
// applying a usable snapshot through Application, the next one-shot start
// selects the snapshot's binding and enters the protocol-start path
// (authenticating). The only difference is the delegated snapshot, so the
// binding and state transition are observable evidence of delegation.
func TestApplicationApplySystemNetworkSnapshotDelegatesToSupervisor(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()

	// Without a network snapshot, a one-shot start cannot select a binding and
	// waits for network.
	firstID, firstSnapshot, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatalf("first StartOneShotAuthentication() error = %v", err)
	}
	if firstSnapshot.SelectedNetworkBinding != nil {
		t.Fatalf("expected no binding before applying snapshot, got %+v", firstSnapshot.SelectedNetworkBinding)
	}
	if firstSnapshot.State != session.WaitingForNetwork {
		t.Errorf("first State = %q, want %q", firstSnapshot.State, session.WaitingForNetwork)
	}
	if _, err := setup.application.StopSession(ctx, firstID); err != nil {
		t.Fatalf("first StopSession() error = %v", err)
	}
	waitForApplicationSessionState(t, setup.application, firstID, session.Suspended)

	// Apply a usable snapshot through Application. It delegates into the same
	// Supervisor used by one-shot start, so the next start selects the binding
	// and enters the protocol-start path.
	snapshot := appTestNetworkSnapshot(t, 1)
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot() error = %v", err)
	}

	secondID, secondSnapshot, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatalf("second StartOneShotAuthentication() error = %v", err)
	}
	if secondSnapshot.SelectedNetworkBinding == nil {
		t.Fatalf("expected selected network binding after applying usable snapshot, got nil")
	}
	if secondSnapshot.SelectedNetworkBinding.InterfaceID != "iface-1" {
		t.Errorf("SelectedNetworkBinding.InterfaceID = %q, want %q",
			secondSnapshot.SelectedNetworkBinding.InterfaceID, "iface-1")
	}
	if secondSnapshot.State != session.Authenticating {
		t.Errorf("second State = %q, want %q", secondSnapshot.State, session.Authenticating)
	}
	if _, err := setup.application.StopSession(ctx, secondID); err != nil {
		t.Fatalf("second StopSession() error = %v", err)
	}
}

func waitForApplicationSessionState(t *testing.T, application *Application, id session.AuthenticationSessionID, want session.State) session.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		snapshot, err := application.GetSession(ctx, id)
		if err != nil {
			t.Fatalf("GetSession() error while waiting for state %q: %v", want, err)
		}
		if snapshot.State == want {
			return snapshot
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for state %q; latest state %q", want, snapshot.State)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
