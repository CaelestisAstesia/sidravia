package application

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	d520description "sidravia/internal/connection/auth/d520"
	d520 "sidravia/internal/connection/auth/d520/driver"
	"sidravia/internal/connection/auth/protocol"
	"sidravia/internal/connection/binding"
	connectionconfig "sidravia/internal/connection/config"
	profilevalue "sidravia/internal/connection/profile"
	profilecatalog "sidravia/internal/connection/profile/catalog"
	config "sidravia/internal/daemon/internal/application/internal/configstore"
	"sidravia/internal/daemon/internal/session"
	"sidravia/internal/document"
)

func TestConfigurationReadModelsKeepUnavailableRowsManageable(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	missingProtocol := appTestProfile("missing-protocol")
	missingProtocol.AuthenticationProtocolID = "unregistered"
	d520Profile := appTestProfile("d520-profile")
	d520Profile.AuthenticationProtocolID = d520description.ProtocolID
	profiles, err := profilecatalog.NewProfileCatalog([]profilevalue.Profile{appTestProfile("profile-1"), missingProtocol, d520Profile})
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
	for _, value := range []connectionconfig.Configuration{
		{ConfigurationID: "broken-profile", InstitutionProfileID: "missing-profile", Username: "user", NetworkBindingPolicy: binding.Policy{Mode: binding.AutomaticallySelectLatestAvailable}, ProtocolContextOverride: profilevalue.ProtocolOverride(`{}`)},
		{ConfigurationID: "broken-protocol", InstitutionProfileID: "missing-protocol", Username: "user", NetworkBindingPolicy: binding.Policy{Mode: binding.AutomaticallySelectLatestAvailable}, ProtocolContextOverride: profilevalue.ProtocolOverride(`{}`)},
		{ConfigurationID: "broken-override", InstitutionProfileID: "d520-profile", Username: "user", NetworkBindingPolicy: binding.Policy{Mode: binding.AutomaticallySelectLatestAvailable}, ProtocolContextOverride: profilevalue.ProtocolOverride(`{"private-legacy-key":"private-value"}`)},
	} {
		if _, err := setup.catalog.Create(ctx, value, "private-password", false); err != nil {
			t.Fatal(err)
		}
	}
	setup.store.mu.Lock()
	disk := make(map[string][]byte)
	for k, v := range setup.store.data {
		disk[k] = bytes.Clone(v)
	}
	setup.store.mu.Unlock()
	rows, protection, err := setup.application.ListConfigurations(ctx)
	if err != nil || len(rows) != 4 || protection != "protected" {
		t.Fatalf("whole list unavailable: %#v %v", rows, err)
	}
	want := map[connectionconfig.ID]ConfigurationRuntimeAvailability{"broken-profile": ConfigurationRuntimeProfileUnavailable, "broken-protocol": ConfigurationRuntimeProtocolUnavailable, "broken-override": ConfigurationRuntimeOverrideInvalid, "configuration-1": ConfigurationRuntimeAvailable}
	for i, row := range rows {
		if row.RuntimeAvailability != want[row.Configuration.ConfigurationID] || !row.CredentialStored {
			t.Fatalf("incorrect row: %#v", row)
		}
		if i > 0 && rows[i-1].Configuration.ConfigurationID >= row.Configuration.ConfigurationID {
			t.Fatal("catalog ordering changed")
		}
		if row.Configuration.ConfigurationID == "broken-profile" {
			if row.InstitutionDisplayName != "" || row.AuthenticationProtocolID != "" || row.Configuration.InstitutionProfileID != "missing-profile" {
				t.Fatal("missing Profile metadata invented")
			}
		} else {
			if row.InstitutionDisplayName == "" || row.AuthenticationProtocolID == "" {
				t.Fatal("known Profile metadata lost")
			}
		}
		fetched, err := setup.application.GetConfiguration(ctx, row.Configuration.ConfigurationID)
		if err != nil || !reflect.DeepEqual(fetched, row) {
			t.Fatalf("unavailable row cannot be queried: %v", err)
		}
	}
	setup.store.mu.Lock()
	unchanged := reflect.DeepEqual(disk, setup.store.data)
	setup.store.mu.Unlock()
	if !unchanged {
		t.Fatal("readonly availability changed persistent bytes")
	}
	name := "edited"
	if _, err := setup.application.UpdateConfiguration(ctx, "broken-profile", config.Update{Patch: connectionconfig.Patch{DisplayName: &name}}); err == nil {
		t.Fatal("fallback used for strict write")
	}
	if _, err := setup.application.SetConfigurationPassword(ctx, "broken-override", "new-private-password", false); err == nil {
		t.Fatal("invalid override write newly accepted")
	}
	var clearOverride profilevalue.ProtocolOverride
	for _, id := range []connectionconfig.ID{"broken-profile", "broken-protocol"} {
		before, err := setup.catalog.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setup.application.UpdateConfiguration(ctx, id, config.Update{Patch: connectionconfig.Patch{ProtocolContextOverride: &clearOverride, DisplayName: &name}}); err == nil {
			t.Fatal("mixed edit bypassed strict candidate validation")
		}
		after, err := setup.catalog.Get(ctx, id)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("mixed edit partially committed")
		}
		cleared, err := setup.application.UpdateConfiguration(ctx, id, config.Update{Patch: connectionconfig.Patch{ProtocolContextOverride: &clearOverride}})
		before.ProtocolContextOverride = nil
		if err != nil || cleared.RuntimeAvailability != want[id] || !reflect.DeepEqual(before, cleared.Configuration) {
			t.Fatalf("isolated clear did not preserve unavailable configuration: %v", err)
		}
	}
	for id := range want {
		if id == "configuration-1" {
			continue
		}
		if err := setup.application.RemoveConfiguration(ctx, id, currentConfigurationVersion(t, setup.catalog, id)); err != nil {
			t.Fatal("broken row cannot be removed")
		}
	}
	remaining, _, err := setup.application.ListConfigurations(ctx)
	if err != nil || len(remaining) != 1 || remaining[0].RuntimeAvailability != ConfigurationRuntimeAvailable {
		t.Fatal("valid row affected by broken rows")
	}
}

type mutatingReadValidator struct{ appTestProtocolFactory }

func (*mutatingReadValidator) ValidateProtocolContextOverride(raw profilevalue.ProtocolOverride) error {
	if len(raw) > 0 {
		raw[0] = '['
	}
	return nil
}
func TestConfigurationReadValidationOwnsInputAndPropagatesUnexpectedErrors(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	registry, err := protocol.NewAuthenticationProtocolRegistry(&mutatingReadValidator{appTestProtocolFactory{id: "drcom"}})
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	value, err := setup.application.GetConfiguration(ctx, "configuration-1")
	if err != nil || value.RuntimeAvailability != ConfigurationRuntimeAvailable || string(value.Configuration.ProtocolContextOverride) != `{}` {
		t.Fatal("validator corrupted row clone")
	}
	value.Configuration.ProtocolContextOverride[0] = '['
	current, err := setup.catalog.Get(ctx, "configuration-1")
	if err != nil || string(current.ProtocolContextOverride) != `{}` {
		t.Fatal("validator/result changed Catalog")
	}
	invalidID := appTestConfiguration("invalid-profile")
	invalidID.InstitutionProfileID = "INVALID"
	if _, err := setup.catalog.Create(ctx, invalidID, "private", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := setup.application.ListConfigurations(ctx); err == nil {
		t.Fatal("invalid lookup silently treated as missing Profile")
	}
	if _, err := setup.application.GetConfiguration(ctx, "invalid-profile"); err == nil {
		t.Fatal("invalid lookup swallowed")
	}
	var clearOverride profilevalue.ProtocolOverride
	if _, err := setup.application.UpdateConfiguration(ctx, "invalid-profile", config.Update{Patch: connectionconfig.Patch{ProtocolContextOverride: &clearOverride}}); err == nil {
		t.Fatal("invalid Profile lookup was accepted")
	}
	after, err := setup.catalog.Get(ctx, "invalid-profile")
	if err != nil || !reflect.DeepEqual(invalidID, after) {
		t.Fatal("clear committed despite returning a Profile lookup error")
	}
	if _, err := setup.application.GetConfiguration(nil, "configuration-1"); err == nil {
		t.Fatal("nil context accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := setup.application.ListConfigurations(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("context failure not propagated")
	}
	if _, err := setup.application.GetConfiguration(ctx, "absent"); err == nil {
		t.Fatal("missing Catalog record fabricated")
	} else {
		var failure *document.Failure
		if !errors.As(err, &failure) || failure.Code() != document.FailureNotFound {
			t.Fatal("Catalog classification changed")
		}
	}
	cause := errors.New("configuration-read-cancel-cause")
	withCause, cancelCause := context.WithCancelCause(ctx)
	cancelCause(cause)
	if _, _, err := setup.application.ListConfigurations(withCause); !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatalf("List lost cancellation cause: %v", err)
	}
	for _, id := range []connectionconfig.ID{"configuration-1", "absent"} {
		if _, err := setup.application.GetConfiguration(withCause, id); !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
			t.Fatalf("Get %q lost cancellation cause: %v", id, err)
		}
	}
	for _, id := range []connectionconfig.ID{"configuration-1", "invalid-profile"} {
		if err := setup.catalog.Delete(ctx, id, currentConfigurationVersion(t, setup.catalog, id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := setup.application.ListConfigurations(withCause); !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatalf("empty List lost cancellation cause: %v", err)
	}
}

func TestSessionListViewOwnershipContextAndFilteredCleanup(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	empty, err := setup.application.ListSessionView(ctx)
	if err != nil || empty.Sessions == nil || empty.CleanupRequiredSessionIDs == nil || len(empty.Sessions) != 0 || len(empty.CleanupRequiredSessionIDs) != 0 {
		t.Fatal("empty owned arrays missing")
	}
	if _, err := setup.application.ListSessionView(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	canceled, cancel := context.WithCancelCause(ctx)
	cause := errors.New("private-cancel-cause")
	cancel(cause)
	if _, err := setup.application.ListSessionView(canceled); !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
		t.Fatalf("original cancellation cause lost: %v", err)
	}
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	setup.application.operationMu.Lock()
	setup.application.invalidSessions["stale-no-actor"] = true
	setup.application.invalidSessions[started.SessionID] = true
	setup.application.operationMu.Unlock()
	view, err := setup.application.ListSessionView(ctx)
	if err != nil || len(view.Sessions) != 1 || len(view.CleanupRequiredSessionIDs) != 1 || view.CleanupRequiredSessionIDs[0] != started.SessionID {
		t.Fatal("cleanup projection includes stale ID")
	}
	actor, err := setup.application.GetSession(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Sessions[0], actor) {
		t.Fatal("projection invented actor facts")
	}
	view.Sessions[0].AuthenticationSessionID = "mutated"
	view.Sessions[0].State = session.Stopping
	view.CleanupRequiredSessionIDs[0] = "mutated"
	if view.Sessions[0].StateReason != nil {
		view.Sessions[0].StateReason.Description = "private-mutated"
	}
	fresh, err := setup.application.ListSessionView(ctx)
	if err != nil || fresh.Sessions[0].AuthenticationSessionID != started.SessionID || fresh.CleanupRequiredSessionIDs[0] != started.SessionID || !reflect.DeepEqual(fresh.Sessions[0], actor) {
		t.Fatal("returned mutable clone aliases owner")
	}
	setup.application.operationMu.Lock()
	locked, err := setup.application.listSessionViewLocked(ctx)
	setup.application.operationMu.Unlock()
	if err != nil || !reflect.DeepEqual(locked, fresh) {
		t.Fatal("already-locked composition changed observation")
	}
	if err := setup.application.RemoveSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	fresh, err = setup.application.ListSessionView(ctx)
	if err != nil || len(fresh.Sessions) != 0 || len(fresh.CleanupRequiredSessionIDs) != 0 {
		t.Fatal("removed actor retains cleanup metadata")
	}
}
