package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence/jsonfile"
)

type appMemoryStore struct{ data map[string][]byte }

type appTestSetup struct {
	authenticationResolver *AuthenticationResolver
	configuration          config.Configuration
}

func newAppMemoryStore() *appMemoryStore { return &appMemoryStore{data: make(map[string][]byte)} }
func (s *appMemoryStore) Read(_ context.Context, path string, _ int64) ([]byte, bool, error) {
	data, ok := s.data[path]
	return append([]byte(nil), data...), ok, nil
}
func (s *appMemoryStore) Replace(_ context.Context, path string, data []byte) error {
	s.data[path] = append([]byte(nil), data...)
	return nil
}
func (s *appMemoryStore) ReplaceSensitive(ctx context.Context, path string, data []byte, _ int64, _ bool) error {
	return s.Replace(ctx, path, data)
}
func (s *appMemoryStore) ProtectionStatus() jsonfile.ProtectionStatus {
	return jsonfile.ProtectionProtected
}

type appTestProtocolFactory struct {
	id protocol.AuthenticationProtocolID
}

func (f *appTestProtocolFactory) ProtocolID() protocol.AuthenticationProtocolID { return f.id }
func (*appTestProtocolFactory) ValidateInstitutionProtocolConfiguration(protocol.InstitutionProtocolConfiguration) error {
	return nil
}
func (*appTestProtocolFactory) ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride) error {
	return nil
}
func (*appTestProtocolFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return blockingRun{}, nil
}

type blockingRun struct{}

func (blockingRun) Execute(ctx context.Context, _ protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	<-ctx.Done()
	return nil
}

func appTestHostInfo() environment.SystemHostInformation {
	return environment.SystemHostInformation{HostName: "test-host", OperatingSystemFamily: "windows", OperatingSystemRelease: "10", MachineArchitecture: "amd64"}
}
func appTestProfile(id config.InstitutionProfileID) config.InstitutionProfile {
	return config.InstitutionProfile{InstitutionProfileID: id, DisplayName: string(id) + " display", AuthenticationProtocolID: "drcom", InstitutionProtocolConfiguration: protocol.InstitutionProtocolConfiguration(`{}`)}
}
func appTestConfiguration(id config.ConfigurationID) config.Configuration {
	return config.Configuration{ConfigurationID: id, DisplayName: string(id) + " display", InstitutionProfileID: "profile-1", Username: "user", NetworkBindingPolicy: config.NetworkBindingPolicy{Mode: config.AutomaticallySelectLatestAvailable}, ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{}`)}
}
func validOneShotInput() OneShotAuthenticationInput {
	return OneShotAuthenticationInput{InstitutionProfileID: "profile-1", AuthenticationCredential: credential.AuthenticationCredential{Username: "user", Password: "secret"}, NetworkBindingPolicy: session.NetworkBindingPolicy{Mode: session.AutomaticallySelectLatestAvailable}, ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{}`)}
}

func newAppTestSetup(t *testing.T) *appTestSetup {
	t.Helper()
	ctx := context.Background()
	store := newAppMemoryStore()
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
	configuration := appTestConfiguration("configuration-1")
	if _, err := catalog.Create(ctx, configuration, "secret", false); err != nil {
		t.Fatal(err)
	}
	return &appTestSetup{authenticationResolver: resolver, configuration: configuration}
}

func TestResolverUsesAggregateCredentialAndClonesDefinition(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Create(ctx, appTestConfiguration("configuration-1"), "secret", false); err != nil {
		t.Fatal(err)
	}
	profiles, _ := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	registry, _ := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	resolver, err := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolver.Resolve(ctx, "configuration-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if definition.AuthenticationCredential.Username != "user" || definition.AuthenticationCredential.Password != "secret" {
		t.Fatal("credential did not resolve")
	}
	if definition.Configuration.AuthenticationSessionID != "session-1" {
		t.Fatal("session id did not resolve")
	}
	if definition.Configuration.ConfigurationID != "configuration-1" {
		t.Fatalf("configuration identity did not resolve: %q", definition.Configuration.ConfigurationID)
	}
	originalOverride := append([]byte(nil), definition.Configuration.ProtocolContextOverride...)
	definition.Configuration.ProtocolContextOverride[0] = '['
	resolvedAgain, err := resolver.Resolve(ctx, "configuration-1", "session-2")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resolvedAgain.Configuration.ProtocolContextOverride, originalOverride) {
		t.Fatal("resolved RuntimeDefinition aliased mutable aggregate bytes")
	}
	public, err := catalog.Get(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("secret")) || bytes.Contains(encoded, []byte("password")) {
		t.Fatalf("public Configuration exposed secret field: %s", encoded)
	}
}

func TestResolverOneShotDoesNotPersistCredential(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, _ := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	profiles, _ := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	registry, _ := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	resolver, _ := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	definition, err := resolver.ResolveOneShot(ctx, validOneShotInput(), "session-1")
	if err != nil || definition.AuthenticationCredential.Password != "secret" {
		t.Fatalf("definition/error = %#v/%v", definition, err)
	}
	if definition.Configuration.ConfigurationID != "" {
		t.Fatalf("one-shot ConfigurationID = %q, want empty", definition.Configuration.ConfigurationID)
	}
	values, _ := catalog.List(ctx)
	if len(values) != 0 {
		t.Fatal("one-shot persisted configuration")
	}
}

func TestResolverFailureClassificationAndSecretSafety(t *testing.T) {
	setup := newAppTestSetup(t)
	for _, test := range []struct {
		name      string
		resolve   func() error
		wantCode  ResolutionFailureCode
		forbidden string
	}{
		{"missing configuration", func() error {
			_, err := setup.authenticationResolver.Resolve(context.Background(), "missing", "session-1")
			return err
		}, ConfigurationNotFound, "secret"},
		{"empty session", func() error {
			_, err := setup.authenticationResolver.Resolve(context.Background(), "configuration-1", "")
			return err
		}, InvalidConfiguration, "secret"},
		{"empty one-shot username", func() error {
			input := validOneShotInput()
			input.AuthenticationCredential.Username = ""
			input.AuthenticationCredential.Password = "private-marker"
			_, err := setup.authenticationResolver.ResolveOneShot(context.Background(), input, "session-1")
			return err
		}, InvalidConfiguration, "private-marker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.resolve()
			var failure *ResolutionFailure
			if !errors.As(err, &failure) || failure.Code() != test.wantCode {
				t.Fatalf("failure = %#v, want %q", err, test.wantCode)
			}
			if strings.Contains(err.Error(), test.forbidden) {
				t.Fatalf("failure leaked protected value: %v", err)
			}
		})
	}
}

func TestApplicationCreateAndUpdateValidateProfileBeforePersistence(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	profiles, _ := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	registry, _ := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	resolver, _ := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	sup := supervisor.New(appSupervisorDeps())
	defer func() { _ = sup.Close(); sup.Wait() }()
	application, err := NewApplication(catalog, profiles, resolver, sup)
	if err != nil {
		t.Fatal(err)
	}
	invalid := appTestConfiguration("invalid-create")
	invalid.InstitutionProfileID = "missing"
	if _, err := application.CreateConfiguration(ctx, invalid, "private", false); err == nil {
		t.Fatal("create with missing Profile succeeded")
	}
	if _, err := catalog.Get(ctx, invalid.ConfigurationID); err == nil {
		t.Fatal("invalid create persisted")
	}
	valid := appTestConfiguration("valid")
	if _, err := application.CreateConfiguration(ctx, valid, "private", false); err != nil {
		t.Fatal(err)
	}
	missing := config.InstitutionProfileID("missing")
	if _, err := application.UpdateConfiguration(ctx, valid.ConfigurationID, config.Update{InstitutionProfileID: &missing}); err == nil {
		t.Fatal("update with missing Profile succeeded")
	}
	got, err := catalog.Get(ctx, valid.ConfigurationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.InstitutionProfileID != "profile-1" {
		t.Fatal("invalid update persisted")
	}
}

func TestResolverPassesAutoReconnectFromConfiguration(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, _ := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	configuration := appTestConfiguration("configuration-1")
	configuration.AutoReconnect = false
	if _, err := catalog.Create(ctx, configuration, "secret", false); err != nil {
		t.Fatal(err)
	}
	profiles, _ := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	registry, _ := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	resolver, _ := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	definition, err := resolver.Resolve(ctx, "configuration-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if definition.AutoReconnect {
		t.Fatal("AutoReconnect = true, want false")
	}
}

func TestResolverOneShotSetsAutoReconnectTrue(t *testing.T) {
	ctx := context.Background()
	store := newAppMemoryStore()
	catalog, _ := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	profiles, _ := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1")})
	registry, _ := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	resolver, _ := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	definition, err := resolver.ResolveOneShot(ctx, validOneShotInput(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !definition.AutoReconnect {
		t.Fatal("one-shot AutoReconnect = false, want true")
	}
}
