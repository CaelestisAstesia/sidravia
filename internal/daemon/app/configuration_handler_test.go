package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/persistence"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
)

func TestConfigurationHandlerListAndPasswordSecrecy(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := ConfigurationHandler(setup.application)
	result, publicErr := handler(context.Background(), contract.MethodConfigurationList, json.RawMessage(`{}`))
	if publicErr != nil {
		t.Fatal(publicErr)
	}
	var list contract.ConfigurationListResult
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Configurations) != 1 || list.Configurations[0].ConfigurationID != "configuration-1" {
		t.Fatalf("list = %#v", list)
	}
	if string(result) == "secret" {
		t.Fatal("password leaked")
	}
}

func TestConfigurationHandlerRejectsUnknownCreateFields(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	_, publicErr := ConfigurationHandler(setup.application)(context.Background(), contract.MethodConfigurationCreate, json.RawMessage(`{"configurationId":"campus","displayName":"","institutionProfileId":"profile-1","username":"user","password":"secret","allowInsecureStorage":false,"extra":true}`))
	if publicErr == nil || publicErr.Code != contract.ErrorCodeInvalidArgument {
		t.Fatalf("error = %#v", publicErr)
	}
}

type fakeConfigurationApplication struct {
	result     ConfigurationResult
	list       []ConfigurationResult
	protection jsonfile.ProtectionStatus
	err        error
	method     string
	id         config.ConfigurationID
	password   string
	allow      bool
	update     config.Update
}

func (fake *fakeConfigurationApplication) ListConfigurations(context.Context) ([]ConfigurationResult, jsonfile.ProtectionStatus, error) {
	fake.method = contract.MethodConfigurationList
	return fake.list, fake.protection, fake.err
}
func (fake *fakeConfigurationApplication) GetConfiguration(_ context.Context, id config.ConfigurationID) (ConfigurationResult, error) {
	fake.method, fake.id = contract.MethodConfigurationGet, id
	return fake.result, fake.err
}
func (fake *fakeConfigurationApplication) CreateConfiguration(_ context.Context, value config.Configuration, password string, allow bool) (ConfigurationResult, error) {
	fake.method, fake.id, fake.password, fake.allow = contract.MethodConfigurationCreate, value.ConfigurationID, password, allow
	return fake.result, fake.err
}
func (fake *fakeConfigurationApplication) UpdateConfiguration(_ context.Context, id config.ConfigurationID, update config.Update) (ConfigurationResult, error) {
	fake.method, fake.id, fake.update = contract.MethodConfigurationUpdate, id, update
	return fake.result, fake.err
}
func (fake *fakeConfigurationApplication) SetConfigurationPassword(_ context.Context, id config.ConfigurationID, password string, allow bool) (ConfigurationResult, error) {
	fake.method, fake.id, fake.password, fake.allow = contract.MethodConfigurationSetPassword, id, password, allow
	return fake.result, fake.err
}
func (fake *fakeConfigurationApplication) RemoveConfiguration(_ context.Context, id config.ConfigurationID) error {
	fake.method, fake.id = contract.MethodConfigurationRemove, id
	return fake.err
}

func completeConfigurationResult() ConfigurationResult {
	return ConfigurationResult{
		Configuration: config.Configuration{
			ConfigurationID: "campus", DisplayName: "校园网", InstitutionProfileID: "jlu",
			Username: "user", NetworkBindingPolicy: config.NetworkBindingPolicy{Mode: config.AutomaticallySelectLatestAvailable},
		},
		InstitutionDisplayName: "吉林大学", AuthenticationProtocolID: "drcom-5.2.0-d",
		CredentialStored: true, StorageProtection: jsonfile.ProtectionProtected,
	}
}

func TestConfigurationHandlerRoutesEveryMethodAndExactResults(t *testing.T) {
	tests := []struct {
		method  string
		payload string
		want    string
	}{
		{contract.MethodConfigurationList, `{}`, `{"storageProtection":"protected","configurations":[]}`},
		{contract.MethodConfigurationGet, `{"configurationId":"campus"}`, ""},
		{contract.MethodConfigurationCreate, `{"configurationId":"campus","displayName":"","institutionProfileId":"jlu","username":"user","password":"","allowInsecureStorage":false,"autoLogin":false,"autoReconnect":false}`, ""},
		{contract.MethodConfigurationUpdate, `{"configurationId":"campus","displayName":""}`, ""},
		{contract.MethodConfigurationSetPassword, `{"configurationId":"campus","password":"","allowInsecureStorage":false}`, ""},
		{contract.MethodConfigurationRemove, `{"configurationId":"campus"}`, `{"configurationId":"campus","status":"removed"}`},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			fake := &fakeConfigurationApplication{result: completeConfigurationResult(), protection: jsonfile.ProtectionProtected}
			result, publicErr := ConfigurationHandler(fake)(context.Background(), tc.method, []byte(tc.payload))
			if publicErr != nil {
				t.Fatal(publicErr)
			}
			if fake.method != tc.method {
				t.Fatalf("routed to %q", fake.method)
			}
			if tc.want != "" && string(result) != tc.want {
				t.Fatalf("result = %s", result)
			}
			if strings.Contains(string(result), "password") {
				t.Fatalf("result exposed password field: %s", result)
			}
		})
	}
	fake := &fakeConfigurationApplication{}
	_, publicErr := ConfigurationHandler(fake)(context.Background(), "configuration.unknown", []byte(`{}`))
	if publicErr == nil || publicErr.Code != contract.ErrorCodeUnknownMethod || fake.method != "" {
		t.Fatalf("unknown route = %#v, method=%q", publicErr, fake.method)
	}
}

func TestConfigurationHandlerMapsStableErrorsWithoutIdentifiersOrCauses(t *testing.T) {
	const causeMarker = "wrapped-cause-marker"
	const idMarker = "private-configuration-id"
	tests := []struct {
		name string
		err  error
		code string
		msg  string
	}{
		{"not found", persistence.NewFailure(persistence.FailureNotFound, errors.New(causeMarker)), contract.ErrorCodeConfigurationNotFound, "configuration not found"},
		{"confirmation", persistence.NewFailure(persistence.FailurePermissionDenied, jsonfile.ErrInsecureStorageConfirmationRequired), contract.ErrorCodeInsecureStorageConfirmationRequired, "insecure storage confirmation required"},
		{"generic", errors.New(causeMarker), contract.ErrorCodeConfigurationOperationFailed, "configuration operation failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeConfigurationApplication{err: tc.err}
			_, publicErr := ConfigurationHandler(fake)(context.Background(), contract.MethodConfigurationGet, []byte(`{"configurationId":"`+idMarker+`"}`))
			if publicErr == nil || publicErr.Code != tc.code || publicErr.Message != tc.msg {
				t.Fatalf("error = %#v", publicErr)
			}
			if strings.Contains(publicErr.Message, causeMarker) || strings.Contains(publicErr.Message, idMarker) {
				t.Fatalf("public error leaked private material: %#v", publicErr)
			}
		})
	}
}
