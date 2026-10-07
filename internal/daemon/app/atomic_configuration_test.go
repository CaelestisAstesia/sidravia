package app

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"reflect"
	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"strings"
	"sync"
	"testing"
	"time"
)

type editFactory struct {
	appTestProtocolFactory
	blocked     bool
	credentials chan protocol.AuthenticationProtocolRunCreationInputs
}

func (f *editFactory) CreateAuthenticationProtocolRun(input protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	f.credentials <- input
	return editRun{blocked: f.blocked}, nil
}

type editRun struct{ blocked bool }

func (r editRun) Execute(ctx context.Context, o protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	if r.blocked {
		return &protocol.AuthenticationProtocolRunFailure{Code: "credential_invalid", Description: "Invalid credentials.", HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange}
	}
	o.AuthenticationEstablished()
	<-ctx.Done()
	return nil
}
func TestApplicationAtomicEditRetiresSessionAndResolvesNewCredentials(t *testing.T) {
	for _, state := range []session.State{session.Authenticated, session.Suspended, session.BlockedByError} {
		t.Run(string(state), func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			ctx := context.Background()
			factory := &editFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}, blocked: state == session.BlockedByError, credentials: make(chan protocol.AuthenticationProtocolRunCreationInputs, 4)}
			registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
			if err != nil {
				t.Fatal(err)
			}
			setup.application.authenticationResolver.protocols = registry
			if err = setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
				t.Fatal(err)
			}
			a, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil {
				t.Fatal(err)
			}
			expected := state
			if state == session.Suspended {
				expected = session.Authenticated
			}
			waitForApplicationSessionState(t, setup.application, a.SessionID, expected)
			<-factory.credentials
			if state == session.Suspended {
				if _, err = setup.application.StopSession(ctx, a.SessionID); err != nil {
					t.Fatal(err)
				}
				waitForApplicationSessionState(t, setup.application, a.SessionID, state)
			}
			username, password := "new-account", "new-password"
			if _, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username, Password: &password}); err != nil {
				t.Fatal(err)
			}
			if _, err = setup.application.GetSession(ctx, a.SessionID); !errors.Is(err, supervisor.ErrSessionNotFound) {
				t.Fatal("old Session retained")
			}
			b, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil || b.SessionID == a.SessionID || b.Snapshot.AccountName != username {
				t.Fatalf("new session = %#v %v", b, err)
			}
			select {
			case input := <-factory.credentials:
				if input.AuthenticationCredential.Username != username || input.AuthenticationCredential.Password != password {
					t.Fatal("resolver reused old credentials")
				}
			case <-time.After(time.Second):
				t.Fatal("no new run")
			}
		})
	}
}
func TestAtomicEditPersistenceFailureKeepsSessionAndDisk(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	a, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	setup.store.mu.Lock()
	before := make(map[string][]byte)
	for k, v := range setup.store.data {
		before[k] = append([]byte(nil), v...)
	}
	setup.store.fail = true
	setup.store.mu.Unlock()
	username, password := "new", "new-password"
	if _, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username, Password: &password}); err == nil {
		t.Fatal("write failure succeeded")
	}
	got, cred, err := setup.catalog.Resolve(ctx, "configuration-1")
	if err != nil || got.Username != "user" || cred.Password != "secret" {
		t.Fatal("partial edit")
	}
	if _, err = setup.application.GetSession(ctx, a.SessionID); err != nil {
		t.Fatal("failed edit retired Session")
	}
	setup.store.mu.Lock()
	defer setup.store.mu.Unlock()
	for k, v := range before {
		if !bytes.Equal(v, setup.store.data[k]) {
			t.Fatal("disk changed")
		}
	}
}
func TestNoSessionAndNoopMetadataEdits(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	username := "user"
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username}); err != nil {
		t.Fatal(err)
	}
	a, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	name := "label"
	if _, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{DisplayName: &name, Username: &username}); err != nil {
		t.Fatal(err)
	}
	if _, err = setup.application.GetSession(ctx, a.SessionID); err != nil {
		t.Fatal("no-op/label edit retired Session")
	}
	if _, err = setup.application.SetConfigurationPassword(ctx, "configuration-1", "new-password", false); err != nil {
		t.Fatal(err)
	}
	if _, err = setup.application.GetSession(ctx, a.SessionID); !errors.Is(err, supervisor.ErrSessionNotFound) {
		t.Fatal("password-only did not retire")
	}
}
func TestCleanupFailureQuarantinesOldRuntime(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	a, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	// A closed supervisor exposes a deterministic cleanup failure after persistence.
	if err = setup.supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	username := "new"
	_, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username})
	if !errors.Is(err, ErrConfigurationSessionInvalidation) {
		t.Fatalf("cleanup failure = %v", err)
	}
	if _, err = setup.application.EnsureSessionRunning(ctx, a.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
		t.Fatal("old ID can ensure")
	}
	if _, err = setup.application.RestartSession(ctx, a.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
		t.Fatal("old ID can restart")
	}
	if _, err = setup.application.StartConfigurationAuthentication(ctx, "configuration-1"); !errors.Is(err, ErrConfigurationSessionInvalidation) {
		t.Fatal("old association can start")
	}
	result := configurationError(errors.Join(ErrConfigurationSessionInvalidation, errors.New("secret-cause")))
	if result.Code != "configuration_session_invalidation_failed" || result.Message != "configuration committed; session cleanup required" {
		t.Fatal("unsafe partial-commit boundary")
	}
}

func TestRuntimeMetadataChangesRetireButAutoLoginDoesNot(t *testing.T) {
	for _, field := range []string{"profile", "autoReconnect", "autoLogin", "binding"} {
		t.Run(field, func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			ctx := context.Background()
			profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1"), appTestProfile("profile-2")})
			if err != nil {
				t.Fatal(err)
			}
			setup.application.profiles = profiles
			setup.application.authenticationResolver.profiles = profiles
			started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil {
				t.Fatal(err)
			}
			update := config.Update{}
			switch field {
			case "binding":
				policy := config.NetworkBindingPolicy{Mode: config.ExplicitInterfaceAndLocalIPv4, InterfaceID: "lo", LocalIPv4Address: netip.MustParseAddr("127.0.0.1")}
				update.NetworkBindingPolicy = &policy
			case "profile":
				id := config.InstitutionProfileID("profile-2")
				update.InstitutionProfileID = &id
			case "autoReconnect":
				value, _ := setup.catalog.Get(ctx, "configuration-1")
				flag := !value.AutoReconnect
				update.AutoReconnect = &flag
			case "autoLogin":
				flag := true
				update.AutoLogin = &flag
			}
			if _, err = setup.application.UpdateConfiguration(ctx, "configuration-1", update); err != nil {
				t.Fatal(err)
			}
			_, err = setup.application.GetSession(ctx, started.SessionID)
			if field == "autoLogin" {
				if err != nil {
					t.Fatal("logon policy retired runtime")
				}
				return
			}
			if !errors.Is(err, supervisor.ErrSessionNotFound) {
				t.Fatal("runtime metadata retained old Session")
			}
			next, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil || next.SessionID == started.SessionID {
				t.Fatalf("next session: %v", err)
			}
		})
	}
}

// The transport Run remains controlled; override validation is delegated to
// the real selected protocol boundary without producing authentication traffic.
type overrideEditFactory struct{ editFactory }

func (*overrideEditFactory) ValidateProtocolContextOverride(raw protocol.AuthenticationProtocolContextOverride) error {
	return d520.NewFactory().ValidateProtocolContextOverride(raw)
}

func TestOverrideSaveRetiresOnlyAfterCommitAndExplicitConnectUsesNewBytes(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	factory := &overrideEditFactory{editFactory: editFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}, credentials: make(chan protocol.AuthenticationProtocolRunCreationInputs, 4)}}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	old, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, old.SessionID, session.Authenticated)
	<-factory.credentials
	invalid := protocol.AuthenticationProtocolContextOverride(`{"schemaVersion":2,"private-marker":"secret"}`)
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{ProtocolContextOverride: &invalid}); err == nil {
		t.Fatal("invalid override saved")
	}
	unchanged, err := setup.catalog.Get(ctx, "configuration-1")
	if err != nil || string(unchanged.ProtocolContextOverride) != `{}` {
		t.Fatal("invalid override partially changed durable data")
	}
	if _, err := setup.application.GetSession(ctx, old.SessionID); err != nil {
		t.Fatal("invalid override retired existing Session")
	}
	select {
	case <-factory.credentials:
		t.Fatal("invalid override created Run")
	default:
	}

	replacement := protocol.AuthenticationProtocolContextOverride(`{"schemaVersion":1,"hostName":"new-reported-host"}`)
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{ProtocolContextOverride: &replacement}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.GetSession(ctx, old.SessionID); !errors.Is(err, supervisor.ErrSessionNotFound) {
		t.Fatal("save retained immutable old Session")
	}
	if list, err := setup.application.ListSessions(ctx); err != nil || len(list) != 0 {
		t.Fatal("save auto-connected")
	}
	select {
	case <-factory.credentials:
		t.Fatal("save created Run")
	default:
	}
	persisted, err := setup.catalog.Get(ctx, "configuration-1")
	if err != nil || !bytes.Equal(persisted.ProtocolContextOverride, replacement) {
		t.Fatal("save not durable")
	}
	next, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil || next.SessionID == old.SessionID {
		t.Fatalf("explicit new connection: %v", err)
	}
	waitForApplicationSessionState(t, setup.application, next.SessionID, session.Authenticated)
	select {
	case input := <-factory.credentials:
		if !bytes.Equal(input.ProtocolContextOverride, replacement) {
			t.Fatal("next Run used old override")
		}
	case <-time.After(time.Second):
		t.Fatal("next explicit Run absent")
	}
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{ProtocolContextOverride: &replacement}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.GetSession(ctx, next.SessionID); err != nil {
		t.Fatal("same runtime bytes retired Session")
	}
	var clear protocol.AuthenticationProtocolContextOverride
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{ProtocolContextOverride: &clear}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.GetSession(ctx, next.SessionID); !errors.Is(err, supervisor.ErrSessionNotFound) {
		t.Fatal("clear retained old runtime")
	}
	select {
	case <-factory.credentials:
		t.Fatal("clear auto-created Run")
	default:
	}
	cleared, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, cleared.SessionID, session.Authenticated)
	select {
	case input := <-factory.credentials:
		if len(input.ProtocolContextOverride) != 0 {
			t.Fatal("clear not used by next Run")
		}
	case <-time.After(time.Second):
		t.Fatal("clear explicit Run absent")
	}
}

func TestRealD520OverrideWriteValidationPreservesLegacyReadAndOldSession(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	old, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	d520Profile := appTestProfile("d520-profile")
	d520Profile.AuthenticationProtocolID = d520.ProtocolID
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1"), d520Profile})
	if err != nil {
		t.Fatal(err)
	}
	setup.application.profiles = profiles
	setup.application.authenticationResolver.profiles = profiles
	registry, err := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"}, d520.NewFactory())
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	value := appTestConfiguration("candidate")
	value.InstitutionProfileID = "d520-profile"
	setup.store.mu.Lock()
	disk := make(map[string][]byte)
	for k, v := range setup.store.data {
		disk[k] = bytes.Clone(v)
	}
	setup.store.mu.Unlock()
	for _, raw := range []string{`{"schemaVersion":2}`, `{"schemaVersion":1,"private-secret-marker":"private-value"}`, `{"schemaVersion":1,"reportedIPv4":"224.0.0.1"}`} {
		value.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(raw)
		_, err := setup.application.CreateConfiguration(ctx, value, "private-password", false)
		var failure *ResolutionFailure
		if !errors.As(err, &failure) || failure.Code() != InvalidConfiguration || failure.Unwrap() == nil {
			t.Fatalf("selected D520 validation missing: %v", err)
		}
		public := configurationError(err)
		if public.Code != "invalid_argument" || strings.Contains(public.Message, "private") {
			t.Fatal("raw override cause leaked")
		}
		if _, err := setup.application.GetSession(ctx, old.SessionID); err != nil {
			t.Fatal("invalid create retired existing Session")
		}
	}
	setup.store.mu.Lock()
	unchanged := reflect.DeepEqual(disk, setup.store.data)
	setup.store.mu.Unlock()
	if !unchanged {
		t.Fatal("invalid selected override wrote store")
	}
	value.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(`{"schemaVersion":1,"hostName":"reported"}`)
	if _, err := setup.application.CreateConfiguration(ctx, value, "private-password", false); err != nil {
		t.Fatal(err)
	}
	// Catalog remains protocol-opaque; legacy read-only paths stay readable.
	opaque := protocol.AuthenticationProtocolContextOverride(`{"private-old-key":true}`)
	if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{ProtocolContextOverride: &opaque}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.GetConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatal("legacy read-only enrichment newly validates override")
	}
	profileID := config.InstitutionProfileID("d520-profile")
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{InstitutionProfileID: &profileID}); err == nil {
		t.Fatal("candidate Profile failed to validate retained override")
	}
	current, _ := setup.catalog.Get(ctx, "configuration-1")
	if current.InstitutionProfileID != "profile-1" || !bytes.Equal(current.ProtocolContextOverride, opaque) {
		t.Fatal("invalid candidate mutated committed record")
	}
	if _, err := setup.application.GetSession(ctx, old.SessionID); err != nil {
		t.Fatal("invalid candidate retired old Session")
	}
}

func TestOverrideWriteFailureAndCleanupFailureRetainExistingAtomicRules(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "store failure", true: "cleanup failure"}[cleanupFailure], func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			ctx := context.Background()
			started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil {
				t.Fatal(err)
			}
			replacement := protocol.AuthenticationProtocolContextOverride(`{"schemaVersion":1,"hostName":"saved"}`)
			if cleanupFailure {
				_ = setup.supervisor.Close()
			} else {
				setup.store.mu.Lock()
				setup.store.fail = true
				setup.store.mu.Unlock()
			}
			_, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{ProtocolContextOverride: &replacement})
			current, readErr := setup.catalog.Get(ctx, "configuration-1")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !cleanupFailure {
				if err == nil || string(current.ProtocolContextOverride) != `{}` {
					t.Fatal("failed store partially saved")
				}
				if _, err := setup.application.GetSession(ctx, started.SessionID); err != nil {
					t.Fatal("failed store retired Session")
				}
				return
			}
			if !errors.Is(err, ErrConfigurationSessionInvalidation) || !bytes.Equal(current.ProtocolContextOverride, replacement) {
				t.Fatal("cleanup failure lost durable replacement")
			}
			if _, err := setup.application.EnsureSessionRunning(ctx, started.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
				t.Fatal("quarantined runtime can ensure")
			}
			if _, err := setup.application.RestartSession(ctx, started.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
				t.Fatal("quarantined runtime can restart")
			}
		})
	}
}

// This Run models a genuine in-progress owned cleanup: cancellation is observed
// promptly, but completion remains controlled until the test releases it.
type cleanupDeadlineFactory struct {
	appTestProtocolFactory
	canceled chan struct{}
	release  <-chan struct{}
}

func (f *cleanupDeadlineFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return cleanupDeadlineRun{canceled: f.canceled, release: f.release}, nil
}

type cleanupDeadlineRun struct {
	canceled chan struct{}
	release  <-chan struct{}
}

func (r cleanupDeadlineRun) Execute(ctx context.Context, observer protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	observer.AuthenticationEstablished()
	<-ctx.Done()
	close(r.canceled)
	<-r.release
	return nil
}
func TestRealCleanupDeadlineIsQueryableOnFirstFullSessionView(t *testing.T) {
	setup := newApplicationTestSetup(t)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRun := func() { releaseOnce.Do(func() { close(release) }) }
	// Registered before any fatal assertion: unblock owned cleanup before Close.
	defer func() { releaseRun(); setup.cleanup() }()
	canceled := make(chan struct{})
	factory := &cleanupDeadlineFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}, canceled: canceled, release: release}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	ctx := context.Background()
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, started.SessionID, session.Authenticated)
	username := "durably-saved-new-user"
	began := time.Now()
	_, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username})
	if !errors.Is(err, ErrConfigurationSessionInvalidation) || !errors.Is(err, context.DeadlineExceeded) || time.Since(began) < 3900*time.Millisecond {
		t.Fatalf("real four-second cleanup boundary missing: %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("owned Run cancellation not observed")
	}
	value, err := setup.catalog.Get(ctx, "configuration-1")
	if err != nil || value.Username != username {
		t.Fatal("cleanup failure lost durable edit")
	}
	query, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	view, err := setup.application.ListSessionView(query)
	if err != nil || len(view.Sessions) != 1 || view.Sessions[0].AuthenticationSessionID != started.SessionID || view.Sessions[0].State != session.Stopping || len(view.CleanupRequiredSessionIDs) != 1 || view.CleanupRequiredSessionIDs[0] != started.SessionID {
		t.Fatalf("first complete readonly query lost stopping/quarantine: %#v %v", view, err)
	}
	if _, err := setup.application.EnsureSessionRunning(ctx, started.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
		t.Fatal("quarantined ID can ensure")
	}
	if _, err := setup.application.RestartSession(ctx, started.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
		t.Fatal("quarantined ID can restart")
	}
	releaseRun()
	remove, cancelRemove := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRemove()
	if err := setup.application.RemoveSession(remove, started.SessionID); err != nil {
		t.Fatal(err)
	}
	fresh, err := setup.application.ListSessionView(ctx)
	if err != nil || fresh.Sessions == nil || fresh.CleanupRequiredSessionIDs == nil || len(fresh.Sessions) != 0 || len(fresh.CleanupRequiredSessionIDs) != 0 {
		t.Fatal("explicit Remove did not clear owner projections")
	}
}
