package app

import (
	"bytes"
	"context"
	"errors"
	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
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
	for _, field := range []string{"profile", "autoReconnect", "autoLogin"} {
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
