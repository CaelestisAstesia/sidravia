package app

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type applicationTestSetup struct {
	application *Application
	catalog     *config.Catalog
	supervisor  *supervisor.Supervisor
	store       *controlledAppStore
}

type controlledAppStore struct {
	mu      sync.Mutex
	data    map[string][]byte
	fail    bool
	block   chan struct{}
	entered chan struct{}
}

func newControlledAppStore() *controlledAppStore {
	return &controlledAppStore{data: make(map[string][]byte)}
}

func (store *controlledAppStore) Read(_ context.Context, path string, _ int64) ([]byte, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	data, ok := store.data[path]
	return append([]byte(nil), data...), ok, nil
}

func (store *controlledAppStore) Replace(ctx context.Context, path string, data []byte) error {
	return store.ReplaceSensitive(ctx, path, data, false)
}

func (store *controlledAppStore) ReplaceSensitive(ctx context.Context, path string, data []byte, _ bool) error {
	store.mu.Lock()
	block, entered := store.block, store.entered
	store.block, store.entered = nil, nil
	store.mu.Unlock()
	if block != nil {
		close(entered)
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.fail {
		return errors.New("controlled replacement failure")
	}
	store.data[path] = append([]byte(nil), data...)
	return nil
}

func (*controlledAppStore) ProtectionStatus() jsonfile.ProtectionStatus {
	return jsonfile.ProtectionProtected
}

func appSupervisorDeps() supervisor.Dependencies {
	return supervisor.Dependencies{
		RetryPolicy: appUnavailableRetryPolicy{}, RetryScheduler: appNoOpRetryScheduler{},
		Now: func() time.Time { return time.Unix(100, 0) },
	}
}

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

func newApplicationTestSetup(t *testing.T) *applicationTestSetup {
	t.Helper()
	ctx := context.Background()
	store := newControlledAppStore()
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(appSupervisorDeps())
	application, err := NewApplication(catalog, profiles, resolver, sup)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Create(ctx, appTestConfiguration("configuration-1"), "secret", false); err != nil {
		t.Fatal(err)
	}
	return &applicationTestSetup{application: application, catalog: catalog, supervisor: sup, store: store}
}
func (setup *applicationTestSetup) cleanup() { _ = setup.supervisor.Close(); setup.supervisor.Wait() }

func appTestNetworkSnapshot(t *testing.T, revision uint64) environment.Snapshot {
	t.Helper()
	iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:              "iface-1",
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{iface})
}

func TestApplicationConfigurationCRUDAndEnrichment(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	values, protection, err := setup.application.ListConfigurations(ctx)
	if err != nil || len(values) != 1 || protection != "protected" {
		t.Fatalf("list = %#v %q %v", values, protection, err)
	}
	if !values[0].CredentialStored || values[0].AuthenticationProtocolID != "drcom" {
		t.Fatalf("result = %#v", values[0])
	}
	name := ""
	updated, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{DisplayName: &name})
	if err != nil || updated.Configuration.DisplayName != "" {
		t.Fatalf("update = %#v %v", updated, err)
	}
	if _, err := setup.application.SetConfigurationPassword(ctx, "configuration-1", "", false); err != nil {
		t.Fatal(err)
	}
}

func TestStartConfigurationEnsuresSameSessionAndRemoveStopsIt(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	firstResult, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil || firstResult.SessionID != secondResult.SessionID || secondResult.Outcome != SessionStartAlreadyRunning {
		t.Fatalf("second = %#v %v, first %#v", secondResult, err, firstResult)
	}
	if err := setup.application.RemoveConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err == nil {
		t.Fatal("configuration remained")
	}
	if _, err := setup.supervisor.Get(ctx, firstResult.SessionID); err == nil {
		t.Fatal("associated Session remained")
	}
}

func TestConfigurationStartActiveAndSuspendedEnsureSameRuntime(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()

	firstResult, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	againResult, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	if againResult.SessionID != firstResult.SessionID || againResult.Snapshot.Revision != firstResult.Snapshot.Revision || againResult.Outcome != SessionStartAlreadyRunning {
		t.Fatalf("active ensure created or revised Session: first=%#v again=%#v", firstResult, againResult)
	}

	changedUser := "new-user"
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &changedUser}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.SetConfigurationPassword(ctx, "configuration-1", "new-password", false); err != nil {
		t.Fatal(err)
	}
	sameResult, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	if sameResult.SessionID != firstResult.SessionID || sameResult.Snapshot.AccountName != "user" {
		t.Fatalf("existing RuntimeDefinition changed: %#v", sameResult)
	}

	if _, err := setup.application.StopSession(ctx, firstResult.SessionID); err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, firstResult.SessionID, session.Suspended)
	resumedResult, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	if resumedResult.SessionID != firstResult.SessionID || resumedResult.Snapshot.State == session.Suspended || resumedResult.Snapshot.AccountName != "user" || resumedResult.Outcome != SessionStartResumed {
		t.Fatalf("suspended ensure did not resume immutable Session: %#v", resumedResult)
	}
}

func TestEnsureSessionRunningReportsActiveAndResumedOutcomes(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()

	started, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatal(err)
	}
	active, err := setup.application.EnsureSessionRunning(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Outcome != SessionStartAlreadyRunning || active.SessionID != started.SessionID || active.Snapshot.AuthenticationSessionID != started.SessionID {
		t.Fatalf("active ensure = %#v", active)
	}

	if _, err := setup.application.StopSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, started.SessionID, session.Suspended)
	resumed, err := setup.application.EnsureSessionRunning(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Outcome != SessionStartResumed || resumed.SessionID != started.SessionID || resumed.Snapshot.AuthenticationSessionID != started.SessionID {
		t.Fatalf("resumed ensure = %#v", resumed)
	}
}

func TestConfigurationEnsureErrorPreservesAssociation(t *testing.T) {
	setup := newApplicationTestSetup(t)
	ctx := context.Background()
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	setup.supervisor.Wait()

	returned, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err == nil {
		t.Fatal("ensure after Supervisor close succeeded")
	}
	setup.application.mu.Lock()
	associated := setup.application.sessionsByConfig["configuration-1"]
	setup.application.mu.Unlock()
	if returned.SessionID != "" || associated != started.SessionID {
		t.Fatalf("ensure error changed association: returned=%#v associated=%q want=%q", returned, associated, started.SessionID)
	}
}

func TestRemoveSessionClearsAssociationButKeepsConfiguration(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.application.RemoveSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err != nil {
		t.Fatalf("RemoveSession deleted Configuration: %v", err)
	}
	setup.application.mu.Lock()
	associated := setup.application.sessionsByConfig["configuration-1"]
	setup.application.mu.Unlock()
	if associated != "" {
		t.Fatalf("RemoveSession retained association %q", associated)
	}
}

func TestRemoveConfigurationPersistenceFailureKeepsAggregateAndRetryDeletes(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	setup.store.mu.Lock()
	setup.store.fail = true
	setup.store.mu.Unlock()
	if err := setup.application.RemoveConfiguration(ctx, "configuration-1"); err == nil {
		t.Fatal("remove succeeded during replacement failure")
	}
	if _, err := setup.supervisor.Get(ctx, started.SessionID); err == nil {
		t.Fatal("Session remained after successful Supervisor removal")
	}
	configuration, credential, err := setup.catalog.Resolve(ctx, "configuration-1")
	if err != nil {
		t.Fatalf("aggregate missing after replacement failure: %v", err)
	}
	if configuration.Username != "user" || credential.Username != "user" || credential.Password != "secret" {
		t.Fatal("aggregate was partially changed after replacement failure")
	}
	setup.application.mu.Lock()
	associated := setup.application.sessionsByConfig["configuration-1"]
	setup.application.mu.Unlock()
	if associated != "" {
		t.Fatalf("association remained after successful Session removal: %q", associated)
	}
	setup.store.mu.Lock()
	setup.store.fail = false
	setup.store.mu.Unlock()
	if err := setup.application.RemoveConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatalf("retry remove: %v", err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err == nil {
		t.Fatal("aggregate remained after retry")
	}
}

func TestConfigurationOperationsSerializeWithSessionRemove(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	started, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatal(err)
	}
	setup.store.mu.Lock()
	setup.store.block = make(chan struct{})
	setup.store.entered = make(chan struct{})
	release, entered := setup.store.block, setup.store.entered
	setup.store.mu.Unlock()
	name := "changed"
	updateDone := make(chan error, 1)
	go func() {
		_, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{DisplayName: &name})
		updateDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("update did not reach persistence boundary")
	}
	removeDone := make(chan error, 1)
	go func() { removeDone <- setup.application.RemoveSession(ctx, started.SessionID) }()
	select {
	case err := <-removeDone:
		t.Fatalf("RemoveSession crossed opMu: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if err := <-removeDone; err != nil {
		t.Fatal(err)
	}
}

func TestOneShotIsIndependentOfConfigurationAssociation(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	started, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.application.RemoveSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.catalog.Get(ctx, "configuration-1"); err != nil {
		t.Fatal(err)
	}
}

func waitForApplicationSessionState(t *testing.T, application *Application, id session.AuthenticationSessionID, want session.State) session.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		value, err := application.GetSession(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if value.State == want {
			return value
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %q", want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestPerformAutomaticLoginNoConfigurationReturnsNil(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	if err := setup.application.PerformAutomaticLogin(ctx); err != nil {
		t.Fatalf("PerformAutomaticLogin() error = %v", err)
	}
}

func TestPerformAutomaticLoginStartsAutoLoginConfiguration(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	enable := true
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoLogin: &enable}); err != nil {
		t.Fatal(err)
	}
	if err := setup.application.PerformAutomaticLogin(ctx); err != nil {
		t.Fatalf("PerformAutomaticLogin() error = %v", err)
	}
	setup.application.mu.Lock()
	sessionID := setup.application.sessionsByConfig["configuration-1"]
	setup.application.mu.Unlock()
	if sessionID == "" {
		t.Fatal("auto-login did not start a Session")
	}
}

func TestPerformAutomaticLoginFailureReturnsSafeError(t *testing.T) {
	setup := newApplicationTestSetup(t)
	ctx := context.Background()
	enable := true
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoLogin: &enable}); err != nil {
		t.Fatal(err)
	}
	if err := setup.supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	setup.supervisor.Wait()
	err := setup.application.PerformAutomaticLogin(ctx)
	if err == nil {
		t.Fatal("PerformAutomaticLogin() unexpectedly succeeded")
	}
	if err.Error() != "automatic_login_failed" {
		t.Fatalf("error = %q, want %q", err.Error(), "automatic_login_failed")
	}
}
