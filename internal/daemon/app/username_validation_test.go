package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
)

// Only transport execution is controlled. Username validation uses the real
// selected D520 implementation, without reading passwords or opening sockets.
type usernameEditFactory struct {
	editFactory
	canceled chan struct{}
}

func (*usernameEditFactory) ValidateUsername(value string) error {
	return d520.NewFactory().ValidateUsername(value)
}
func (f *usernameEditFactory) CreateAuthenticationProtocolRun(input protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	f.credentials <- input
	return usernameEditRun{done: f.canceled}, nil
}

type usernameEditRun struct{ done chan struct{} }

func (r usernameEditRun) Execute(ctx context.Context, o protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	o.AuthenticationEstablished()
	<-ctx.Done()
	if r.done != nil {
		close(r.done)
	}
	return nil
}
func usernameTestFactory(t *testing.T, setup *applicationTestSetup) *usernameEditFactory {
	t.Helper()
	f := &usernameEditFactory{editFactory: editFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}, credentials: make(chan protocol.AuthenticationProtocolRunCreationInputs, 4)}, canceled: make(chan struct{})}
	registry, err := protocol.NewAuthenticationProtocolRegistry(f)
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	return f
}
func usernameDisk(setup *applicationTestSetup) map[string][]byte {
	setup.store.mu.Lock()
	defer setup.store.mu.Unlock()
	result := make(map[string][]byte, len(setup.store.data))
	for k, v := range setup.store.data {
		result[k] = bytes.Clone(v)
	}
	return result
}
func requireUsernameRejection(t *testing.T, err error) {
	t.Helper()
	var failure *ResolutionFailure
	if !errors.As(err, &failure) || failure.Code() != InvalidConfiguration || failure.Unwrap() == nil {
		t.Fatalf("missing typed validation cause: %v", err)
	}
}

func TestUsernameCreateUsesRealD520WithoutRunOrNetwork(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	profile := appTestProfile("d520-profile")
	profile.AuthenticationProtocolID = d520.ProtocolID
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := protocol.NewAuthenticationProtocolRegistry(d520.NewFactory())
	if err != nil {
		t.Fatal(err)
	}
	setup.application.profiles = profiles
	setup.application.authenticationResolver.protocols = registry
	value := appTestConfiguration("candidate")
	value.InstitutionProfileID = profile.InstitutionProfileID
	before := usernameDisk(setup)
	for _, username := range []string{strings.Repeat("a", 37), strings.Repeat("中", 19), "invalid😀"} {
		value.Username = username
		_, err := setup.application.CreateConfiguration(ctx, value, "private-password", false)
		requireUsernameRejection(t, err)
		if !reflect.DeepEqual(before, usernameDisk(setup)) {
			t.Fatal("rejected create wrote persistence")
		}
		values, err := setup.catalog.List(ctx)
		if err != nil || len(values) != 1 {
			t.Fatal("rejected create added catalog record or credential")
		}
	}
	value.Username = strings.Repeat("a", 36)
	created, err := setup.application.CreateConfiguration(ctx, value, "private-password", false)
	if err != nil || created.Configuration.Username != value.Username {
		t.Fatalf("boundary create: %v", err)
	}
	if sessions, err := setup.application.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatal("saving created Session")
	}
}

func TestUsernameRejectedUpdatePreservesActiveRunAndValidUpdateRetires(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	f := usernameTestFactory(t, setup)
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	old, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, old.SessionID, session.Authenticated)
	<-f.credentials
	before := usernameDisk(setup)
	snapshot, err := setup.application.GetSession(ctx, old.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	username, password, name := strings.Repeat("a", 37), "replacement-password", "replacement-label"
	_, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username, Password: &password, DisplayName: &name})
	requireUsernameRejection(t, err)
	value, credential, err := setup.catalog.Resolve(ctx, "configuration-1")
	if err != nil || value.Username != "user" || value.DisplayName == name || credential.Password != "secret" {
		t.Fatal("rejected edit changed configuration or credentials")
	}
	if !reflect.DeepEqual(before, usernameDisk(setup)) {
		t.Fatal("rejected update wrote persistence")
	}
	after, err := setup.application.GetSession(ctx, old.SessionID)
	if err != nil || !reflect.DeepEqual(snapshot, after) {
		t.Fatal("rejected update changed original Session")
	}
	select {
	case <-f.canceled:
		t.Fatal("rejected update canceled original Run")
	default:
	}
	select {
	case <-f.credentials:
		t.Fatal("rejected update created Run")
	default:
	}
	username = strings.Repeat("a", 36)
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username, Password: &password}); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.GetSession(ctx, old.SessionID); !errors.Is(err, supervisor.ErrSessionNotFound) {
		t.Fatal("valid edit retained old Session")
	}
	value, credential, err = setup.catalog.Resolve(ctx, "configuration-1")
	if err != nil || value.Username != username || credential.Password != password {
		t.Fatal("valid edit not committed")
	}
	select {
	case <-f.canceled:
	default:
		t.Fatal("old Run not canceled")
	}
	select {
	case <-f.credentials:
		t.Fatal("valid save auto-connected")
	default:
	}
	// Give the next Run its own cancellation signal; the previous Run is joined.
	f.canceled = make(chan struct{})
	next, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil || next.SessionID == old.SessionID {
		t.Fatalf("explicit next connection: %v", err)
	}
	select {
	case input := <-f.credentials:
		if input.AuthenticationCredential.Username != username || input.AuthenticationCredential.Password != password {
			t.Fatal("next Run used stale credentials")
		}
	case <-time.After(time.Second):
		t.Fatal("explicit connection did not create Run")
	}
}

func TestUsernameCandidateUsesNewProfileAndLegacyRemainsReadable(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	strict := &usernameEditFactory{editFactory: editFactory{appTestProtocolFactory: appTestProtocolFactory{id: "strict"}, credentials: make(chan protocol.AuthenticationProtocolRunCreationInputs, 1)}}
	registry, err := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"}, strict)
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	profile := appTestProfile("strict-profile")
	profile.AuthenticationProtocolID = "strict"
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1"), profile})
	if err != nil {
		t.Fatal(err)
	}
	setup.application.profiles = profiles
	setup.application.authenticationResolver.profiles = profiles
	long := strings.Repeat("a", 37)
	if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{Username: &long}); err != nil {
		t.Fatal(err)
	}
	before := usernameDisk(setup)
	_, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{InstitutionProfileID: &profile.InstitutionProfileID})
	requireUsernameRejection(t, err)
	if !reflect.DeepEqual(before, usernameDisk(setup)) {
		t.Fatal("rejected Profile change committed")
	}
	// Seed a legacy invalid selected-protocol row through the protocol-opaque Catalog.
	if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{InstitutionProfileID: &profile.InstitutionProfileID}); err != nil {
		t.Fatal(err)
	}
	before = usernameDisk(setup)
	if _, err := setup.application.GetConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatal("legacy row not readable")
	}
	if rows, _, err := setup.application.ListConfigurations(ctx); err != nil || len(rows) != 1 {
		t.Fatal("legacy row not listable")
	}
	label := "new-label"
	_, err = setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{DisplayName: &label})
	requireUsernameRejection(t, err)
	if !reflect.DeepEqual(before, usernameDisk(setup)) {
		t.Fatal("legacy partial edit wrote invalid candidate")
	}
	repaired := strings.Repeat("a", 36)
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &repaired}); err != nil {
		t.Fatal("legacy username cannot be corrected")
	}
	if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{Username: &long}); err != nil {
		t.Fatal(err)
	}
	if err := setup.application.RemoveConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatal("legacy invalid row not removable")
	}
}
