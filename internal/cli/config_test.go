package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestConfigurationPasswordCallsClearOwnedWireBytes(t *testing.T) {
	cause := errors.New("fictional transport failure")
	for _, method := range []string{contract.MethodConfigurationCreate, contract.MethodConfigurationSetPassword} {
		for _, outcome := range []string{"success", "transport", "typed error"} {
			t.Run(method+"/"+outcome, func(t *testing.T) {
				var retained json.RawMessage
				connection := &fakeDaemonClient{call: func(gotMethod string, buffer json.RawMessage) (contract.Response, error) {
					retained = buffer
					var value struct {
						Password string `json:"password"`
					}
					if gotMethod != method || json.Unmarshal(buffer, &value) != nil || value.Password != "fictional" {
						t.Fatal("wire payload changed before Call consumed it")
					}
					switch outcome {
					case "transport":
						return contract.Response{}, cause
					case "typed error":
						return contract.NewErrorResponse("1", contract.ErrorCodeInvalidArgument, "invalid"), nil
					default:
						return contract.NewSuccessResponse("1", json.RawMessage(`{}`)), nil
					}
				}}
				var payload any = contract.ConfigurationSetPasswordPayload{ConfigurationID: "campus", Password: "fictional"}
				if method == contract.MethodConfigurationCreate {
					payload = contract.ConfigurationCreatePayload{ConfigurationID: "campus", InstitutionProfileID: "jlu", Username: "user", Password: "fictional"}
				}
				_, err := callConfiguration(hotAuthDependencies(t, connection), connection, method, payload)
				if (err == nil) != (outcome == "success") || (outcome == "transport" && !errors.Is(err, cause)) {
					t.Fatal("operation error behavior changed")
				}
				assertClearedBytes(t, retained)
			})
		}
	}
}

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
		configSetPassword: func(configPasswordOptions) error { return nil }, configRemove: func(string, bool, bool) error { return nil },
		output: &bytes.Buffer{},
	}
	if err := runCommand([]string{"config", "create", "--id", "campus", "--profile", "jlu", "--username", "user", "--password", "secret"}, deps); err == nil || !strings.HasPrefix(err.Error(), "用法错误，请运行 sidraviactl help config create") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigCreateSendsAutoLoginAutoReconnectDefaults(t *testing.T) {
	var capturedMethod string
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedMethod = method
		capturedPayload = append(json.RawMessage(nil), payload...)
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
		capturedPayload = append(json.RawMessage(nil), payload...)
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
		capturedPayload = append(json.RawMessage(nil), payload...)
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
		capturedPayload = append(json.RawMessage(nil), payload...)
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
		capturedPayload = append(json.RawMessage(nil), payload...)
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
		capturedPayload = append(json.RawMessage(nil), payload...)
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

func TestReadOnlyConfigAcquisitionFailureWritesNothing(t *testing.T) {
	var output bytes.Buffer
	deps := hotAuthDependencies(t, &fakeDaemonClient{})
	deps.stdout = &output
	deps.connection.acquire = func(context.Context) (daemonClient, error) { return nil, errors.New("stopped") }
	if err := runConfigList(deps); err == nil {
		t.Fatal("stopped config list returned nil")
	}
	if err := runConfigShow("campus", deps); err == nil {
		t.Fatal("stopped config show returned nil")
	}
	if output.Len() != 0 {
		t.Fatalf("output=%q", output.String())
	}
}

func TestUpdateAndRemoveExposeExplicitInsecureStorageConsent(t *testing.T) {
	var update configUpdateOptions
	var removeConsent bool
	deps := commandDependencies{output: &bytes.Buffer{}, configUpdate: func(o configUpdateOptions) error { update = o; return nil }, configRemove: func(_ string, _ bool, allow bool) error { removeConsent = allow; return nil }}
	if err := runCommand([]string{"config", "update", "campus", "--username", "new", "--allow-insecure-storage"}, deps); err != nil {
		t.Fatal(err)
	}
	if !update.allowInsecure || update.username == nil || *update.username != "new" {
		t.Fatal("update consent lost")
	}
	if err := runCommand([]string{"config", "remove", "campus", "--yes", "--allow-insecure-storage"}, deps); err != nil {
		t.Fatal(err)
	}
	if !removeConsent {
		t.Fatal("remove consent lost")
	}
}
