package app

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence"
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
	return store.ReplaceSensitive(ctx, path, data, 0, false)
}

func (store *controlledAppStore) ReplaceSensitive(ctx context.Context, path string, data []byte, _ int64, _ bool) error {
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
	if _, err := catalog.Create(ctx, appTestConfiguration("configuration-1"), "secret", false); err != nil {
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
	if !values[0].CredentialStored || values[0].AuthenticationProtocolID != "drcom" || values[0].RuntimeAvailability != ConfigurationRuntimeAvailable {
		t.Fatalf("result = %#v", values[0])
	}
	name := ""
	updated, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{DisplayName: &name})
	if err != nil || updated.Configuration.DisplayName != "" || updated.RuntimeAvailability != ConfigurationRuntimeAvailable {
		t.Fatalf("update = %#v %v", updated, err)
	}
	if _, err := setup.application.SetConfigurationPassword(ctx, "configuration-1", "", false); err != nil {
		t.Fatal(err)
	}
	values, _, err = setup.application.ListConfigurations(ctx)
	if err != nil || len(values) != 1 || !values[0].CredentialStored {
		t.Fatalf("empty-password credential projection = %#v, %v", values, err)
	}
}

func TestApplicationCreateConfigurationReturnsGeneratedPersistedIdentity(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	value := appTestConfiguration("")
	value.DisplayName = ""
	created, err := setup.application.CreateConfiguration(context.Background(), value, "private", false)
	if err != nil {
		t.Fatal(err)
	}
	if created.RuntimeAvailability != ConfigurationRuntimeAvailable {
		t.Fatal("valid create availability missing")
	}
	if len(created.Configuration.ConfigurationID) != len("cfg-")+32 || created.Configuration.ConfigurationID[:4] != "cfg-" {
		t.Fatalf("generated Configuration = %#v", created.Configuration)
	}
	persisted, err := setup.catalog.Get(context.Background(), created.Configuration.ConfigurationID)
	if err != nil || persisted.ConfigurationID != created.Configuration.ConfigurationID || persisted.DisplayName != "" {
		t.Fatalf("persisted generated Configuration = %#v, %v", persisted, err)
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

	// Direct catalog edits prove the Session actor retains its cloned runtime.
	// Application edits are separately tested to retire that immutable Session.
	changedUser := "new-user"
	if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{Username: &changedUser}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.catalog.SetPassword(ctx, "configuration-1", "new-password", false); err != nil {
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
	if _, err := setup.supervisor.Get(ctx, started.SessionID); err != nil {
		t.Fatal("Session changed before successful persistence")
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
	if associated != started.SessionID {
		t.Fatalf("association changed after failed persistence: %q", associated)
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
	operations := []struct {
		name string
		call func(context.Context, *Application, session.AuthenticationSessionID) error
	}{
		{
			name: "StopSession",
			call: func(ctx context.Context, application *Application, id session.AuthenticationSessionID) error {
				_, err := application.StopSession(ctx, id)
				return err
			},
		},
		{
			name: "EnsureSessionRunning",
			call: func(ctx context.Context, application *Application, id session.AuthenticationSessionID) error {
				_, err := application.EnsureSessionRunning(ctx, id)
				return err
			},
		},
		{
			name: "RestartSession",
			call: func(ctx context.Context, application *Application, id session.AuthenticationSessionID) error {
				_, err := application.RestartSession(ctx, id)
				return err
			},
		},
		{
			name: "RemoveSession",
			call: func(ctx context.Context, application *Application, id session.AuthenticationSessionID) error {
				return application.RemoveSession(ctx, id)
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
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

			var releaseOnce sync.Once
			releasePersistence := func() { releaseOnce.Do(func() { close(release) }) }
			name := "changed"
			updateDone := make(chan error, 1)
			operationDone := make(chan error, 1)
			updateStarted, operationStarted := false, false
			defer func() {
				releasePersistence()
				if updateStarted {
					select {
					case <-updateDone:
					case <-time.After(2 * time.Second):
						t.Error("timed out draining configuration update")
					}
				}
				if operationStarted {
					select {
					case <-operationDone:
					case <-time.After(2 * time.Second):
						t.Error("timed out draining Session operation")
					}
				}
			}()

			updateStarted = true
			go func() {
				_, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{DisplayName: &name})
				updateDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("update did not reach persistence boundary")
			}

			operationStarted = true
			callStarted := make(chan struct{})
			go func() {
				close(callStarted)
				operationDone <- operation.call(ctx, setup.application, started.SessionID)
			}()
			<-callStarted
			waitForApplicationOperationLock(t, operation.name, operationDone)

			releasePersistence()
			select {
			case err := <-updateDone:
				updateStarted = false
				if err != nil {
					t.Fatalf("configuration update: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for configuration update")
			}
			select {
			case err := <-operationDone:
				operationStarted = false
				if err != nil {
					t.Fatalf("%s after Configuration persistence: %v", operation.name, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("timed out waiting for %s", operation.name)
			}
		})
	}
}

// Observing the public call directly parked in Mutex.Lock distinguishes actual
// serialization from a goroutine that simply has not been scheduled. This is a
// test-only observation of the pinned Go runtime, with no production hook.
func waitForApplicationOperationLock(t *testing.T, name string, done <-chan error) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	method := "sidravia/internal/daemon/app.(*Application)." + name + "("
	buffer := make([]byte, 1<<20)
	for {
		select {
		case err := <-done:
			t.Fatalf("%s crossed the closed persistence gate: %v", name, err)
		default:
		}
		stacks := string(buffer[:runtime.Stack(buffer, true)])
		for _, stack := range strings.Split(stacks, "\n\n") {
			methodAt := strings.Index(stack, method)
			if methodAt < 0 {
				continue
			}
			prefix := stack[:methodAt]
			lockAt := strings.LastIndex(prefix, "sync.(*Mutex).Lock(")
			if lockAt >= 0 && strings.Count(prefix[lockAt:], "\n") == 2 {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("%s never entered the operation mutex", name)
		case <-tick.C:
		}
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
	if sessions, err := setup.application.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatal("no automatic configuration must not create a Session")
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
	var failure *automaticLoginFailure
	if !errors.As(err, &failure) || failure.Unwrap() == nil || failure.Unwrap().Error() != "supervisor is closed" {
		t.Fatal("automatic login lost closed supervisor cause")
	}
}

// automaticSelectionContext pauses catalog selection without a production hook.
// The edit's validation callback reports whether it passed the app operation gate.
type automaticSelectionContext struct {
	context.Context
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (c *automaticSelectionContext) Err() error {
	if c.calls.Add(1) == 2 {
		close(c.entered)
		select {
		case <-c.release:
		case <-c.Context.Done():
		}
	}
	return c.Context.Err()
}

type editAdmissionContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *editAdmissionContext) Err() error {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Err()
}
func TestAutomaticLoginSerializesSelection(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	enable := true
	if _, e := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoLogin: &enable}); e != nil {
		t.Fatal(e)
	}
	autoctx := &automaticSelectionContext{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
	autodone := make(chan error, 1)
	go func() { autodone <- setup.application.PerformAutomaticLogin(autoctx) }()
	select {
	case <-autoctx.entered:
	case <-ctx.Done():
		t.Fatal("automatic selection did not begin")
	}
	editctx := &editAdmissionContext{Context: ctx, entered: make(chan struct{})}
	editdone := make(chan error, 1)
	disable, user, password := false, "new-user", "new-password"
	go func() {
		_, e := setup.application.UpdateConfiguration(editctx, "configuration-1", config.Update{AutoLogin: &disable, Username: &user, Password: &password})
		editdone <- e
	}()
	crossed := false
	select {
	case <-editctx.entered:
		crossed = true
	case <-time.After(100 * time.Millisecond):
	}
	close(autoctx.release)
	if e := <-autodone; e != nil {
		t.Fatal(e)
	}
	if e := <-editdone; e != nil {
		t.Fatal(e)
	}
	sessions, e := setup.application.ListSessions(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if crossed {
		t.Error("edit entered while automatic selection was in progress")
	}
	if len(sessions) != 0 {
		t.Fatal("disabled configuration retained automatic Session after credential edit")
	}
	_, cred, e := setup.catalog.Resolve(ctx, "configuration-1")
	if e != nil || cred.Username != user || cred.Password != password {
		t.Fatal("edit did not commit current credentials")
	}
}
func TestAutomaticLoginUsesCommittedChoice(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	on, off := true, false
	if _, e := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoLogin: &on}); e != nil {
		t.Fatal(e)
	}
	user, password := "new-user", "new-password"
	if _, e := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoLogin: &off, Username: &user, Password: &password}); e != nil {
		t.Fatal(e)
	}
	if e := setup.application.PerformAutomaticLogin(ctx); e != nil {
		t.Fatal(e)
	}
	sessions, e := setup.application.ListSessions(ctx)
	if e != nil || len(sessions) != 0 {
		t.Fatal("committed disable started a Session")
	}
	b := appTestConfiguration("configuration-b")
	b.AutoLogin = true
	if _, e := setup.application.CreateConfiguration(ctx, b, "b-password", false); e != nil {
		t.Fatal(e)
	}
	if e := setup.application.PerformAutomaticLogin(ctx); e != nil {
		t.Fatal(e)
	}
	sessions, e = setup.application.ListSessions(ctx)
	if e != nil || len(sessions) != 1 || sessions[0].ConfigurationID != "configuration-b" {
		t.Fatal("automatic login did not follow committed choice")
	}
}

// Err fails at an existing context boundary, without a production injection hook.
type automaticFailureContext struct {
	context.Context
	calls, failAt int
	cause         error
}

func (ctx *automaticFailureContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.failAt {
		return ctx.cause
	}
	return nil
}

func TestAutomaticLoginRetainsListAndStartCausesWithSafeText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		start  bool
	}{
		{"list", 1, false}, {"start resolution", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			if tc.start {
				enable := true
				if _, err := setup.application.UpdateConfiguration(context.Background(), "configuration-1", config.Update{AutoLogin: &enable}); err != nil {
					t.Fatal(err)
				}
			}
			cause := errors.New("account-marker password-marker /private/path cause-marker")
			ctx := &automaticFailureContext{Context: context.Background(), failAt: tc.failAt, cause: cause}
			err := setup.application.PerformAutomaticLogin(ctx)
			if err == nil || err.Error() != "automatic_login_failed" {
				t.Fatalf("unsafe failure: %v", err)
			}
			var safe *automaticLoginFailure
			var original *persistence.Failure
			if !errors.As(err, &safe) || !errors.As(err, &original) || original.DiagnosticCause() != cause {
				t.Fatal("original persistence failure/cause lost")
			}
			if tc.start {
				var resolution *ResolutionFailure
				if !errors.As(err, &resolution) || safe.Unwrap() != resolution || resolution.Unwrap() != original {
					t.Fatal("start return object was replaced")
				}
			} else if safe.Unwrap() != original {
				t.Fatal("list return object was replaced")
			}
			if sessions, err := setup.application.ListSessions(context.Background()); err != nil || len(sessions) != 0 {
				t.Fatal("failure created a Session")
			}
		})
	}
}

func TestAutomaticLoginFailureUnwrapPreservesSentinelIdentity(t *testing.T) {
	cause := errors.New("account-marker password-marker /private/path cause-marker")
	err := &automaticLoginFailure{cause: cause}
	if err.Unwrap() != cause || !errors.Is(err, cause) || err.Error() != "automatic_login_failed" {
		t.Fatal("safe wrapper lost identity or echoed cause")
	}
}
