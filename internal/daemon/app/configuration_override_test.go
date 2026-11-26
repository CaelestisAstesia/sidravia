package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	launchcontract "sidravia/internal/background/launch"
	d520description "sidravia/internal/connection/auth/d520"
	d520 "sidravia/internal/connection/auth/d520/driver"
	"sidravia/internal/connection/auth/protocol"
	profilevalue "sidravia/internal/connection/profile"
	profilecatalog "sidravia/internal/connection/profile/catalog"
	"sidravia/internal/ipc/contract"
)

// Use the real D520 override validator with the existing offline session factory.
type overrideTestFactory struct{ appTestProtocolFactory }

func (*overrideTestFactory) OverrideSchemaVersion() int { return 1 }
func (*overrideTestFactory) ValidateProtocolContextOverride(value profilevalue.ProtocolOverride) error {
	return d520.NewFactory().ValidateProtocolContextOverride(value)
}

func TestPersistentOverrideIPCReadValidateAndVersionProtection(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	profile := appTestProfile("d520")
	profile.AuthenticationProtocolID = d520description.ProtocolID
	profiles, err := profilecatalog.NewProfileCatalog([]profilevalue.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := protocol.NewAuthenticationProtocolRegistry(&overrideTestFactory{appTestProtocolFactory{id: d520description.ProtocolID}})
	if err != nil {
		t.Fatal(err)
	}
	setup.application.profiles = profiles
	setup.application.authenticationResolver.profiles = profiles
	setup.application.authenticationResolver.protocols = registry
	handler := IPCHandler(setup.application, "version", "build", launchcontract.Headless())
	call := func(method string, payload any, wantError string) json.RawMessage {
		t.Helper()
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		result, publicErr := handler(ctx, method, data)
		if wantError != "" {
			if publicErr == nil || publicErr.Code != wantError {
				t.Fatalf("%s: want %s, got %v", method, wantError, publicErr)
			}
		} else if publicErr != nil {
			t.Fatalf("%s: %v", method, publicErr)
		}
		return result
	}
	const document = "{\n  \"schemaVersion\": 1,\n  \"hostName\": \"private-host\"\n}"
	created := call(contract.MethodConfigurationCreate, map[string]any{
		"configurationId": "override", "institutionProfileId": "d520", "username": "user",
		"password": "private-password", "networkBindingPolicy": map[string]string{"mode": "automatically_select_latest_available"},
		"allowInsecureStorage": false, "autoLogin": false, "autoReconnect": true, "overrideDocument": document,
	}, "")
	var saved contract.ConfigurationResult
	if err := json.Unmarshal(created, &saved); err != nil || saved.Version == "" || saved.OverrideSchemaVersion != 1 || saved.OverrideState != "enabled" || strings.Contains(string(created), "private-host") {
		t.Fatalf("unexpected summary: %s, %v", created, err)
	}
	compactDocument := `{"schemaVersion":1,"hostName":"private-host"}`
	id := contract.ConfigurationReadOverridePayload{ConfigurationID: "override", ExpectedDocument: &compactDocument}
	read := call(contract.MethodConfigurationGetOverride, id, "")
	var view contract.ConfigurationOverrideResult
	if err := json.Unmarshal(read, &view); err != nil || view.Document == nil || *view.Document != document ||
		view.MatchesDocument == nil || !*view.MatchesDocument || view.Configuration.Version != saved.Version || strings.Contains(string(read), "private-password") {
		t.Fatalf("unexpected edit read: %v", err)
	}
	invalid := call(contract.MethodConfigurationValidateOverride, contract.ConfigurationValidateOverridePayload{InstitutionProfileID: "d520", Document: `{"schemaVersion":1,"hostName":42}`}, "")
	var validation contract.ConfigurationOverrideValidationResult
	if err := json.Unmarshal(invalid, &validation); err != nil || validation.Valid || validation.Field != "hostName" {
		t.Fatalf("invalid field result: %s", invalid)
	}
	call(contract.MethodConfigurationUpdate, map[string]any{"configurationId": "override", "expectedVersion": saved.Version, "overrideDocument": "null"}, contract.ErrorCodeInvalidArgument)
	// A credential-only edit must invalidate the editor's read and saved start tokens.
	call(contract.MethodConfigurationSetPassword, contract.ConfigurationSetPasswordPayload{ConfigurationID: "override", Password: "new-password"}, "")
	call(contract.MethodConfigurationUpdate, map[string]any{"configurationId": "override", "expectedVersion": saved.Version, "protocolContextOverride": nil}, contract.ErrorCodeConfigurationVersionConflict)
	call(contract.MethodSessionStartConfiguration, contract.ConfigurationStartPayload{ConfigurationID: "override", ExpectedVersion: saved.Version}, contract.ErrorCodeConfigurationVersionConflict)
	call(contract.MethodSessionStartOneShot, map[string]any{
		"institutionProfileId": "d520", "username": "user", "password": "synthetic",
		"networkBindingPolicy":    map[string]string{"mode": "automatically_select_latest_available"},
		"protocolContextOverride": map[string]any{},
	}, contract.ErrorCodeInvalidArgument)
	sessions, err := setup.application.ListSessionView(ctx)
	if err != nil || len(sessions.Sessions) != 0 {
		t.Fatalf("rejected request changed session state: %v", err)
	}
	current, err := setup.application.GetConfiguration(ctx, "override")
	if err != nil || string(current.Configuration.ProtocolContextOverride) != document || current.Version == saved.Version {
		t.Fatalf("stale edit affected saved document: %v", err)
	}
	start := call(contract.MethodSessionStartConfiguration, contract.ConfigurationStartPayload{ConfigurationID: "override", ExpectedVersion: current.Version}, "")
	var sessionResult contract.SessionStartResult
	if err := json.Unmarshal(start, &sessionResult); err != nil || !sessionResult.Session.HasProtocolOverride {
		t.Fatalf("persisted override missing from start status: %v", err)
	}
	cleared := call(contract.MethodConfigurationUpdate, map[string]any{"configurationId": "override", "expectedVersion": current.Version, "protocolContextOverride": nil}, "")
	var summary contract.ConfigurationResult
	if err := json.Unmarshal(cleared, &summary); err != nil || summary.OverrideState != "none" || summary.Version == current.Version {
		t.Fatalf("unexpected clear result: %s", cleared)
	}
	sessions, err = setup.application.ListSessionView(ctx)
	if err != nil || len(sessions.Sessions) != 0 {
		t.Fatalf("clearing did not retire the related session: %v", err)
	}
	// Legacy documents could exceed the editor transport budget. Their version
	// and presence remain readable, and the explicit clear remains available.
	legacy := current.Configuration.Clone()
	legacy.ConfigurationID = "legacy"
	legacy.ProtocolContextOverride = profilevalue.ProtocolOverride(`{"unknown":"` + strings.Repeat("<", 70*1024) + `"}`)
	if _, err := setup.catalog.Create(ctx, legacy, "private-password", false); err != nil {
		t.Fatal(err)
	}
	unreadable := call(contract.MethodConfigurationGetOverride, contract.ConfigurationIDPayload{ConfigurationID: "legacy"}, "")
	view = contract.ConfigurationOverrideResult{}
	if err := json.Unmarshal(unreadable, &view); err != nil || view.Document != nil || view.Configuration.OverrideState != "invalid" || view.Configuration.Version == "" {
		t.Fatalf("legacy document state unavailable: %v", err)
	}
	call(contract.MethodConfigurationUpdate, map[string]any{"configurationId": "legacy", "expectedVersion": view.Configuration.Version, "protocolContextOverride": nil}, "")

}
