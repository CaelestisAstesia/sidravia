package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestConfigurationDecodersAndPresentationHidePassword(t *testing.T) {
	raw, err := contract.MarshalConfigurationResult(contract.ConfigurationResult{
		ConfigurationID: "campus", DisplayName: "校园网", InstitutionProfileID: "jlu",
		InstitutionDisplayName: "吉林大学", AuthenticationProtocolID: "drcom-5.2.0-d",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeConfiguration(&output, result); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !bytes.Contains([]byte(got), []byte("凭据：已保存")) || bytes.Contains([]byte(got), []byte("password")) {
		t.Fatalf("output = %q", got)
	}
}

func TestConfigurationDecodersRejectUnknownTrailingAndIncompleteResults(t *testing.T) {
	valid := contract.ConfigurationResult{
		ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
	}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte{}, data...), []byte("{}")...),
		[]byte(`{"configurationId":"campus","institutionProfileId":"jlu","authenticationProtocolId":"drcom","username":"user","credentialStored":true,"storageProtection":"protected","password":"secret"}`),
		[]byte(`{"configurationId":"campus"}`),
	} {
		if _, err := decodeConfiguration(invalid); err == nil {
			t.Fatalf("accepted invalid configuration result: %s", invalid)
		}
	}
	list, err := json.Marshal(contract.ConfigurationListResult{StorageProtection: "protected", Configurations: []contract.ConfigurationResult{valid}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeConfigurationList(list); err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
	if _, err := decodeConfigurationList([]byte(`{"storageProtection":"protected","configurations":null}`)); err == nil {
		t.Fatal("nil configuration list accepted")
	}
}

func TestConfigurationNonInteractiveRequirementsAreStableAndSecretFree(t *testing.T) {
	deps := authDependencies{
		stdin: strings.NewReader("secret\n"), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
		inputIsConsole:    func(io.Reader) bool { return false },
		readStdinPassword: readPasswordStdin,
	}
	err := runConfigCreate(configCreateOptions{id: "campus", profile: "jlu", username: "user"}, deps)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("create error = %v", err)
	}
	err = runConfigSetPassword(configPasswordOptions{id: "campus"}, deps)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("set-password error = %v", err)
	}
	err = runConfigRemove("campus", false, deps)
	if err == nil {
		t.Fatal("non-interactive remove without --yes accepted")
	}
}

func TestConfigCreateRejectsPasswordOnArgvShape(t *testing.T) {
	deps := commandDependencies{
		configList: func() error { return nil }, configShow: func(string) error { return nil },
		configCreate: func(configCreateOptions) error { return nil }, configUpdate: func(configUpdateOptions) error { return nil },
		configSetPassword: func(configPasswordOptions) error { return nil }, configRemove: func(string, bool) error { return nil },
		output: &bytes.Buffer{},
	}
	if err := runCommand([]string{"config", "create", "--id", "campus", "--profile", "jlu", "--username", "user", "--password", "secret"}, deps); err != errCommandUsage {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigCreateSendsAutoLoginAutoReconnectDefaults(t *testing.T) {
	var capturedMethod string
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedMethod = method
		capturedPayload = payload
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{
			ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: false, AutoReconnect: true,
		})
		return contract.NewSuccessResponse("1", result), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.stdin = strings.NewReader("password\n")
	deps.readStdinPassword = readPasswordStdin
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) { return "password", nil }
	deps.inputIsConsole = func(io.Reader) bool { return false }

	options := configCreateOptions{id: "campus", profile: "jlu", username: "user", passwordStdin: true, autoReconnect: true}
	if err := runConfigCreate(options, deps); err != nil {
		t.Fatal(err)
	}
	if capturedMethod != contract.MethodConfigurationCreate {
		t.Fatalf("method = %q, want %q", capturedMethod, contract.MethodConfigurationCreate)
	}
	var request contract.ConfigurationCreatePayload
	if err := json.Unmarshal(capturedPayload, &request); err != nil {
		t.Fatal(err)
	}
	if request.AutoLogin || !request.AutoReconnect {
		t.Fatalf("defaults: autoLogin=%v, autoReconnect=%v", request.AutoLogin, request.AutoReconnect)
	}
}

func TestConfigCreateExplicitFlagsOverrideDefaults(t *testing.T) {
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedPayload = payload
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{
			ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: true, AutoReconnect: false,
		})
		return contract.NewSuccessResponse("1", result), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.stdin = strings.NewReader("password\n")
	deps.readStdinPassword = readPasswordStdin
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) { return "password", nil }
	deps.inputIsConsole = func(io.Reader) bool { return false }

	options := configCreateOptions{id: "campus", profile: "jlu", username: "user", passwordStdin: true,
		autoLogin: true, autoReconnect: false, autoLoginExplicit: true, autoReconnectExplicit: true}
	if err := runConfigCreate(options, deps); err != nil {
		t.Fatal(err)
	}
	var request contract.ConfigurationCreatePayload
	if err := json.Unmarshal(capturedPayload, &request); err != nil {
		t.Fatal(err)
	}
	if !request.AutoLogin || request.AutoReconnect {
		t.Fatalf("explicit: autoLogin=%v, autoReconnect=%v", request.AutoLogin, request.AutoReconnect)
	}
}

func TestConfigCreateInteractivePromptsAutoLoginAutoReconnect(t *testing.T) {
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedPayload = payload
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{
			ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: true, AutoReconnect: false,
		})
		return contract.NewSuccessResponse("1", result), nil
	}}
	deps := hotAuthDependencies(t, connection)
	// Input: id, profile selection (1), username, auto-login (y), auto-reconnect (n), password
	deps.stdin = strings.NewReader("campus\n1\nuser\ny\nn\n")
	var stderr bytes.Buffer
	deps.stderr = &stderr
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) { return "password", nil }
	deps.inputIsConsole = func(io.Reader) bool { return true }
	// Profile list response for selectProfile
	profileRaw, _ := contract.MarshalProfileListResult(contract.ProfileListResult{Profiles: []contract.ProfileSummaryResult{
		{InstitutionProfileID: "jlu", DisplayName: "吉林大学", AuthenticationProtocolID: "drcom"},
	}})

	var callCount int
	connection.call = func(method string, payload json.RawMessage) (contract.Response, error) {
		callCount++
		if method == contract.MethodProfileList {
			return contract.NewSuccessResponse("1", profileRaw), nil
		}
		if method == contract.MethodConfigurationList {
			listRaw, _ := contract.MarshalConfigurationListResult(contract.ConfigurationListResult{StorageProtection: "protected", Configurations: nil})
			return contract.NewSuccessResponse("1", listRaw), nil
		}
		capturedPayload = payload
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{
			ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: true, AutoReconnect: false,
		})
		return contract.NewSuccessResponse("1", result), nil
	}

	options := configCreateOptions{passwordStdin: false}
	if err := runConfigCreate(options, deps); err != nil {
		t.Fatalf("runConfigCreate error = %v", err)
	}
	var request contract.ConfigurationCreatePayload
	if err := json.Unmarshal(capturedPayload, &request); err != nil {
		t.Fatal(err)
	}
	if !request.AutoLogin || request.AutoReconnect {
		t.Fatalf("interactive: autoLogin=%v, autoReconnect=%v", request.AutoLogin, request.AutoReconnect)
	}
	if !strings.Contains(stderr.String(), "自动登录") || !strings.Contains(stderr.String(), "自动重连") {
		t.Fatalf("stderr missing prompts: %q", stderr.String())
	}
}

func TestConfigCreateNonInteractiveSkipsPrompts(t *testing.T) {
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedPayload = payload
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{
			ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: false, AutoReconnect: true,
		})
		return contract.NewSuccessResponse("1", result), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.stdin = strings.NewReader("")
	var stderr bytes.Buffer
	deps.stderr = &stderr
	deps.readStdinPassword = readPasswordStdin
	deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) { return "password", nil }
	deps.inputIsConsole = func(io.Reader) bool { return false }

	options := configCreateOptions{id: "campus", profile: "jlu", username: "user", passwordStdin: true}
	if err := runConfigCreate(options, deps); err != nil {
		t.Fatal(err)
	}
	if len(capturedPayload) == 0 {
		t.Fatal("create payload was not captured")
	}
	if strings.Contains(stderr.String(), "自动登录") || strings.Contains(stderr.String(), "自动重连") {
		t.Fatalf("non-interactive prompted: %q", stderr.String())
	}
}

func TestConfigUpdateSendsAutoLoginAutoReconnectFlags(t *testing.T) {
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedPayload = payload
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{
			ConfigurationID: "campus", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: true, AutoReconnect: false,
		})
		return contract.NewSuccessResponse("1", result), nil
	}}
	deps := hotAuthDependencies(t, connection)
	deps.inputIsConsole = func(io.Reader) bool { return false }

	autoLogin := true
	autoReconnect := false
	options := configUpdateOptions{id: "campus", autoLogin: &autoLogin, autoReconnect: &autoReconnect}
	if err := runConfigUpdate(options, deps); err != nil {
		t.Fatal(err)
	}
	var request contract.ConfigurationUpdatePayload
	if err := json.Unmarshal(capturedPayload, &request); err != nil {
		t.Fatal(err)
	}
	if request.AutoLogin == nil || !*request.AutoLogin || request.AutoReconnect == nil || *request.AutoReconnect {
		t.Fatalf("update: autoLogin=%v, autoReconnect=%v", request.AutoLogin, request.AutoReconnect)
	}
}
