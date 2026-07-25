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
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/environment"
)

type appMemoryStore struct {
	data map[string][]byte
}

func newAppMemoryStore() *appMemoryStore {
	return &appMemoryStore{data: make(map[string][]byte)}
}

func (store *appMemoryStore) Read(_ context.Context, path string, _ int64) ([]byte, bool, error) {
	data, exists := store.data[path]
	return append([]byte(nil), data...), exists, nil
}

func (store *appMemoryStore) Replace(_ context.Context, path string, data []byte) error {
	store.data[path] = append([]byte(nil), data...)
	return nil
}

type appTestProtocolFactory struct {
	id protocol.AuthenticationProtocolID
}

func (factory *appTestProtocolFactory) ProtocolID() protocol.AuthenticationProtocolID {
	return factory.id
}
func (factory *appTestProtocolFactory) ValidateInstitutionProtocolConfiguration(_ protocol.InstitutionProtocolConfiguration) error {
	return nil
}
func (factory *appTestProtocolFactory) ValidateProtocolContextOverride(_ protocol.AuthenticationProtocolContextOverride) error {
	return nil
}
func (factory *appTestProtocolFactory) CreateAuthenticationProtocolRun(_ protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return blockingRun{}, nil
}

type blockingRun struct{}

func (blockingRun) Execute(ctx context.Context, _ protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	<-ctx.Done()
	return nil
}

func appTestHostInfo() environment.SystemHostInformation {
	return environment.SystemHostInformation{
		HostName:               "test-host",
		OperatingSystemFamily:  "linux",
		OperatingSystemRelease: "6.1",
		MachineArchitecture:    "amd64",
	}
}

func appTestProfile(id config.InstitutionProfileID) config.InstitutionProfile {
	return config.InstitutionProfile{
		InstitutionProfileID:             id,
		DisplayName:                      string(id) + " display",
		AuthenticationProtocolID:         "drcom",
		InstitutionProtocolConfiguration: protocol.InstitutionProtocolConfiguration(`{"realm":"` + string(id) + `"}`),
	}
}

func appTestConfiguration(id config.ConfigurationID) config.Configuration {
	return config.Configuration{
		ConfigurationID:         id,
		DisplayName:             string(id) + " display",
		InstitutionProfileID:    "profile-1",
		CredentialID:            "credential-1",
		NetworkBindingPolicy:    config.NetworkBindingPolicy{Mode: config.AutomaticallySelectLatestAvailable},
		ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{}`),
	}
}

type appTestSetup struct {
	authenticationResolver *AuthenticationResolver
	catalog                *config.Catalog
	credStore              *credential.Store
	profileCat             *config.ProfileCatalog
	registry               *protocol.AuthenticationProtocolRegistry
	sessionID              session.AuthenticationSessionID
	configuration          config.Configuration
}

func newAppTestSetup(t *testing.T) *appTestSetup {
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
	configuration := appTestConfiguration("configuration-1")
	if err := catalog.Save(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	if err := credStore.Put(ctx, "credential-1", credential.AuthenticationCredential{Username: "user", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	return &appTestSetup{
		authenticationResolver: authenticationResolver,
		catalog:                catalog,
		credStore:              credStore,
		profileCat:             profileCat,
		registry:               registry,
		sessionID:              "session-1",
		configuration:          configuration,
	}
}

func TestResolverResolvesSuccessfully(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	definition, err := setup.authenticationResolver.Resolve(ctx, "configuration-1", setup.sessionID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if definition.Configuration.AuthenticationSessionID != "session-1" {
		t.Fatalf("AuthenticationSessionID = %q", definition.Configuration.AuthenticationSessionID)
	}
	if definition.Configuration.DisplayName != "configuration-1 display" {
		t.Fatalf("DisplayName = %q", definition.Configuration.DisplayName)
	}
	if definition.Configuration.CredentialID != "credential-1" {
		t.Fatalf("CredentialID = %q, want %q", definition.Configuration.CredentialID, "credential-1")
	}
	if definition.AuthenticationCredential.Username != "user" {
		t.Fatalf("Username = %q", definition.AuthenticationCredential.Username)
	}
	if definition.InstitutionProfile.DisplayName != "profile-1 display" {
		t.Fatalf("ProfileDisplayName = %q", definition.InstitutionProfile.DisplayName)
	}
	if definition.AuthenticationProtocolFactory.ProtocolID() != "drcom" {
		t.Fatalf("ProtocolID = %q", definition.AuthenticationProtocolFactory.ProtocolID())
	}
	if definition.SystemHostInformation.HostName != "test-host" {
		t.Fatalf("HostName = %q", definition.SystemHostInformation.HostName)
	}
}

func TestResolverConfigurationNotFound(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	_, err := setup.authenticationResolver.Resolve(ctx, "missing", setup.sessionID)
	if code := resolutionFailureCode(t, err); code != ConfigurationNotFound {
		t.Fatalf("Resolve(missing) code = %v, want %v", code, ConfigurationNotFound)
	}
}

func TestResolverCredentialNotFound(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	configWithoutCredential := setup.configuration
	configWithoutCredential.CredentialID = "missing-credential"
	configWithoutCredential.ConfigurationID = "configuration-2"
	if err := setup.catalog.Save(ctx, configWithoutCredential); err != nil {
		t.Fatal(err)
	}
	_, err := setup.authenticationResolver.Resolve(ctx, "configuration-2", setup.sessionID)
	if code := resolutionFailureCode(t, err); code != CredentialNotFound {
		t.Fatalf("Resolve(missing credential) code = %v, want %v", code, CredentialNotFound)
	}
}

func TestResolverProfileNotFound(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	configWithMissingProfile := setup.configuration
	configWithMissingProfile.InstitutionProfileID = "missing-profile"
	configWithMissingProfile.ConfigurationID = "configuration-3"
	if err := setup.catalog.Save(ctx, configWithMissingProfile); err != nil {
		t.Fatal(err)
	}
	_, err := setup.authenticationResolver.Resolve(ctx, "configuration-3", setup.sessionID)
	if code := resolutionFailureCode(t, err); code != ProfileNotFound {
		t.Fatalf("Resolve(missing profile) code = %v, want %v", code, ProfileNotFound)
	}
}

func TestResolverProtocolNotFound(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	unknownProfile := config.InstitutionProfile{
		InstitutionProfileID:             "profile-unknown",
		DisplayName:                      "unknown",
		AuthenticationProtocolID:         "nonexistent",
		InstitutionProtocolConfiguration: protocol.InstitutionProtocolConfiguration(`{}`),
	}
	profileCat, err := config.NewProfileCatalog([]config.InstitutionProfile{unknownProfile})
	if err != nil {
		t.Fatal(err)
	}
	authenticationResolver, err := NewAuthenticationResolver(setup.catalog, profileCat, setup.credStore, setup.registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	configUnknown := setup.configuration
	configUnknown.InstitutionProfileID = "profile-unknown"
	configUnknown.ConfigurationID = "configuration-4"
	if err := setup.catalog.Save(ctx, configUnknown); err != nil {
		t.Fatal(err)
	}
	_, resolveErr := authenticationResolver.Resolve(ctx, "configuration-4", setup.sessionID)
	if code := resolutionFailureCode(t, resolveErr); code != ProtocolNotFound {
		t.Fatalf("Resolve(unknown protocol) code = %v, want %v", code, ProtocolNotFound)
	}
}

func TestResolverRejectsEmptySessionID(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	_, err := setup.authenticationResolver.Resolve(ctx, "configuration-1", "")
	if code := resolutionFailureCode(t, err); code != InvalidConfiguration {
		t.Fatalf("Resolve(empty session) code = %v, want %v", code, InvalidConfiguration)
	}
}

func TestNewResolverRejectsInvalidEnvironment(t *testing.T) {
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
	registry, err := protocol.NewAuthenticationProtocolRegistry(&appTestProtocolFactory{id: "drcom"})
	if err != nil {
		t.Fatal(err)
	}
	_, newErr := NewAuthenticationResolver(catalog, profileCat, credStore, registry, environment.SystemHostInformation{})
	if code := resolutionFailureCode(t, newErr); code != InvalidEnvironment {
		t.Fatalf("NewAuthenticationResolver(empty host) code = %v, want %v", code, InvalidEnvironment)
	}
}

func TestResolverPublicErrorSecrecy(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	_, err := setup.authenticationResolver.Resolve(ctx, "missing", setup.sessionID)
	if err != nil && strings.Contains(err.Error(), "secret") {
		t.Fatalf("public error leaked secret: %v", err)
	}
}

func resolutionFailureCode(t *testing.T, err error) ResolutionFailureCode {
	t.Helper()
	var failure *ResolutionFailure
	if errors.As(err, &failure) {
		return failure.Code()
	}
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	t.Fatalf("error is not a ResolutionFailure: %v", err)
	return ""
}

func validOneShotInput() OneShotAuthenticationInput {
	return OneShotAuthenticationInput{
		DisplayName:          "one-shot display",
		InstitutionProfileID: "profile-1",
		AuthenticationCredential: credential.AuthenticationCredential{
			Username: "oneshot-user",
			Password: "oneshot-password",
		},
		NetworkBindingPolicy:    session.NetworkBindingPolicy{Mode: session.AutomaticallySelectLatestAvailable},
		ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(`{"network":"campus-one-shot"}`),
	}
}

// appJSONValidatingFactory is an AuthenticationProtocolFactory that validates
// opaque JSON, mirroring a real factory's contract checks. It exercises the
// invalid-context-override resolution path without standing up a real protocol.
type appJSONValidatingFactory struct {
	id protocol.AuthenticationProtocolID
}

func (factory *appJSONValidatingFactory) ProtocolID() protocol.AuthenticationProtocolID {
	return factory.id
}
func (factory *appJSONValidatingFactory) ValidateInstitutionProtocolConfiguration(configuration protocol.InstitutionProtocolConfiguration) error {
	return appValidateJSON(configuration)
}
func (factory *appJSONValidatingFactory) ValidateProtocolContextOverride(override protocol.AuthenticationProtocolContextOverride) error {
	return appValidateJSON(override)
}
func (factory *appJSONValidatingFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return blockingRun{}, nil
}

func appValidateJSON(value []byte) error {
	if json.Valid(value) {
		return nil
	}
	return errors.New("invalid JSON")
}

func TestResolverResolveOneShotSuccess(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	input := validOneShotInput()

	definition, err := setup.authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if err != nil {
		t.Fatalf("ResolveOneShot() error = %v", err)
	}
	if definition.Configuration.AuthenticationSessionID != "pending" {
		t.Fatalf("AuthenticationSessionID = %q, want %q", definition.Configuration.AuthenticationSessionID, "pending")
	}
	if definition.Configuration.DisplayName != "one-shot display" {
		t.Fatalf("DisplayName = %q", definition.Configuration.DisplayName)
	}
	if definition.Configuration.CredentialID != "" {
		t.Fatalf("CredentialID = %q, want empty for one-shot", definition.Configuration.CredentialID)
	}
	if definition.Configuration.InstitutionProfileID != "profile-1" {
		t.Fatalf("InstitutionProfileID = %q", definition.Configuration.InstitutionProfileID)
	}
	if definition.InstitutionProfile.InstitutionProfileID != "profile-1" {
		t.Fatalf("resolved profile ID = %q", definition.InstitutionProfile.InstitutionProfileID)
	}
	if definition.InstitutionProfile.DisplayName != "profile-1 display" {
		t.Fatalf("resolved profile display = %q", definition.InstitutionProfile.DisplayName)
	}
	if definition.AuthenticationProtocolFactory.ProtocolID() != "drcom" {
		t.Fatalf("ProtocolID = %q", definition.AuthenticationProtocolFactory.ProtocolID())
	}
	if definition.AuthenticationCredential.Username != "oneshot-user" {
		t.Fatalf("Username = %q", definition.AuthenticationCredential.Username)
	}
	if definition.AuthenticationCredential.Password != "oneshot-password" {
		t.Fatalf("Password = %q", definition.AuthenticationCredential.Password)
	}
	if definition.SystemHostInformation.HostName != "test-host" {
		t.Fatalf("HostName = %q", definition.SystemHostInformation.HostName)
	}
	// The raw protocol override is cloned into the definition, not aliased to
	// the caller's input slice.
	if !bytes.Equal(definition.Configuration.ProtocolContextOverride, input.ProtocolContextOverride) {
		t.Fatal("protocol context override was not copied into the definition")
	}
	definition.Configuration.ProtocolContextOverride[0] ^= 0xff
	if bytes.Equal(definition.Configuration.ProtocolContextOverride, input.ProtocolContextOverride) {
		t.Fatal("definition aliases the caller's protocol context override")
	}
}

func TestResolverResolveOneShotRejectsEmptyUsername(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	input := validOneShotInput()
	input.AuthenticationCredential.Username = ""
	_, err := setup.authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if code := resolutionFailureCode(t, err); code != InvalidConfiguration {
		t.Fatalf("ResolveOneShot(empty username) code = %v, want %v", code, InvalidConfiguration)
	}
}

func TestResolverResolveOneShotRejectsNilContext(t *testing.T) {
	setup := newAppTestSetup(t)
	_, err := setup.authenticationResolver.ResolveOneShot(nil, validOneShotInput(), "pending")
	if code := resolutionFailureCode(t, err); code != InvalidConfiguration {
		t.Fatalf("ResolveOneShot(nil ctx) code = %v, want %v", code, InvalidConfiguration)
	}
}

func TestResolverResolveOneShotRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	setup := newAppTestSetup(t)
	_, err := setup.authenticationResolver.ResolveOneShot(ctx, validOneShotInput(), "pending")
	if code := resolutionFailureCode(t, err); code != InvalidConfiguration {
		t.Fatalf("ResolveOneShot(cancelled ctx) code = %v, want %v", code, InvalidConfiguration)
	}
}

func TestResolverResolveOneShotProfileNotFound(t *testing.T) {
	tests := []struct {
		name    string
		profile config.InstitutionProfileID
	}{
		{name: "empty profile ID", profile: ""},
		{name: "missing profile", profile: "missing-profile"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			setup := newAppTestSetup(t)
			input := validOneShotInput()
			input.InstitutionProfileID = test.profile
			_, err := setup.authenticationResolver.ResolveOneShot(ctx, input, "pending")
			if code := resolutionFailureCode(t, err); code != ProfileNotFound {
				t.Fatalf("ResolveOneShot(%s) code = %v, want %v", test.name, code, ProfileNotFound)
			}
		})
	}
}

func TestResolverResolveOneShotProtocolNotFound(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	unknownProfile := config.InstitutionProfile{
		InstitutionProfileID:             "profile-unknown",
		DisplayName:                      "unknown",
		AuthenticationProtocolID:         "nonexistent",
		InstitutionProtocolConfiguration: protocol.InstitutionProtocolConfiguration(`{}`),
	}
	profileCat, err := config.NewProfileCatalog([]config.InstitutionProfile{unknownProfile})
	if err != nil {
		t.Fatal(err)
	}
	authenticationResolver, err := NewAuthenticationResolver(setup.catalog, profileCat, setup.credStore, setup.registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	input := validOneShotInput()
	input.InstitutionProfileID = "profile-unknown"
	_, err = authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if code := resolutionFailureCode(t, err); code != ProtocolNotFound {
		t.Fatalf("ResolveOneShot(unknown protocol) code = %v, want %v", code, ProtocolNotFound)
	}
}

func TestResolverResolveOneShotRejectsUnsupportedBindingPolicy(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	input := validOneShotInput()
	input.NetworkBindingPolicy.Mode = "manual"
	_, err := setup.authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if code := resolutionFailureCode(t, err); code != InvalidConfiguration {
		t.Fatalf("ResolveOneShot(unsupported binding) code = %v, want %v", code, InvalidConfiguration)
	}
}

func TestResolverResolveOneShotRejectsInvalidContextOverride(t *testing.T) {
	ctx := context.Background()
	setup := newAppTestSetup(t)
	registry, err := protocol.NewAuthenticationProtocolRegistry(&appJSONValidatingFactory{id: "drcom"})
	if err != nil {
		t.Fatal(err)
	}
	authenticationResolver, err := NewAuthenticationResolver(setup.catalog, setup.profileCat, setup.credStore, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	input := validOneShotInput()
	input.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(`invalid`)
	_, err = authenticationResolver.ResolveOneShot(ctx, input, "pending")
	if code := resolutionFailureCode(t, err); code != InvalidConfiguration {
		t.Fatalf("ResolveOneShot(invalid override) code = %v, want %v", code, InvalidConfiguration)
	}
}
