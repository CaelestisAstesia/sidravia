package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
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
	raw, err := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", DisplayName: "校园网", InstitutionProfileID: "jlu",
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
	valid := contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
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
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
			NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
			ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
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
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
			NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
			ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
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
	networkRaw, _ := contract.MarshalNetworkInterfacesResult(contract.NetworkInterfacesResult{
		Available: false, Interfaces: []contract.NetworkInterfaceResult{},
	})
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedPayload = append(json.RawMessage(nil), payload...)
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
			NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
			ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
			Username: "user", CredentialStored: true, StorageProtection: "protected",
			AutoLogin: true, AutoReconnect: false,
		})
		return contract.NewSuccessResponse("1", result), nil
	}}
	deps := hotAuthDependencies(t, connection)
	// Input: id, profile selection (1), username, auto-login (y), auto-reconnect (n),
	// network binding (automatic), then password.
	deps.stdin = strings.NewReader("campus\n1\nuser\ny\nn\n\n")
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
		if method == contract.MethodNetworkInterfaces {
			return contract.NewSuccessResponse("1", networkRaw), nil
		}
		if method == contract.MethodProfileList {
			return contract.NewSuccessResponse("1", profileRaw), nil
		}
		if method == contract.MethodConfigurationList {
			listRaw, _ := contract.MarshalConfigurationListResult(contract.ConfigurationListResult{StorageProtection: "protected", Configurations: []contract.ConfigurationResult{}})
			return contract.NewSuccessResponse("1", listRaw), nil
		}
		capturedPayload = append(json.RawMessage(nil), payload...)
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
			NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
			ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
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

func TestConsoleDetectionRejectsNonConsoleInputs(t *testing.T) {
	regular, err := os.CreateTemp(t.TempDir(), "console-check")
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipeReader.Close()
	defer pipeWriter.Close()
	for name, input := range map[string]io.Reader{
		"regular file": regular,
		"null device":  null,
		"pipe":         pipeReader,
		"reader":       strings.NewReader("input"),
	} {
		t.Run(name, func(t *testing.T) {
			if isConsoleInput(input) {
				t.Fatal("non-console input reported as a console")
			}
		})
	}
}

func TestInteractiveBindingChooserFailuresStopBeforePasswordAndCreate(t *testing.T) {
	validNoFacts, err := contract.MarshalNetworkInterfacesResult(contract.NetworkInterfacesResult{
		Available: false, Interfaces: []contract.NetworkInterfaceResult{},
	})
	if err != nil {
		t.Fatal(err)
	}
	queryCause := errors.New("network query failed")
	for _, test := range []struct {
		name     string
		input    string
		output   io.Writer
		response contract.Response
		callErr  error
	}{
		{name: "query failure", input: "0\n", callErr: queryCause},
		{name: "typed query error", input: "0\n", response: contract.NewErrorResponse("1", contract.ErrorCodeInvalidArgument, "safe")},
		{name: "decode failure", input: "0\n", response: contract.NewSuccessResponse("1", json.RawMessage(`{"available":true}`))},
		{name: "invalid choice", input: "1\n", response: contract.NewSuccessResponse("1", validNoFacts)},
		{name: "negative choice", input: "-1\n", response: contract.NewSuccessResponse("1", validNoFacts)},
		{name: "nonnumeric choice", input: "x\n", response: contract.NewSuccessResponse("1", validNoFacts)},
		{name: "EOF", input: "", response: contract.NewSuccessResponse("1", validNoFacts)},
		{name: "output failure", input: "0\n", output: failingOutputWriter{}, response: contract.NewSuccessResponse("1", validNoFacts)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var methods []string
			connection := &fakeDaemonClient{call: func(method string, _ json.RawMessage) (contract.Response, error) {
				methods = append(methods, method)
				if method != contract.MethodNetworkInterfaces {
					t.Fatalf("unexpected daemon method %q", method)
				}
				return test.response, test.callErr
			}}
			deps := hotAuthDependencies(t, connection)
			input := test.input
			if test.name != "EOF" {
				input += "password\n"
			}
			deps.stdin = strings.NewReader(input)
			deps.inputIsConsole = func(io.Reader) bool { return true }
			if test.output != nil {
				deps.stderr = test.output
			}
			deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) {
				t.Fatal("password was read after chooser failure")
				return "", nil
			}
			options := configCreateOptions{
				id: "campus", profile: "jlu", username: "user",
				autoLoginExplicit: true, autoReconnectExplicit: true,
			}
			if err := runConfigCreate(options, deps); err == nil {
				t.Fatal("chooser failure was accepted")
			}
			if len(methods) != 1 || methods[0] != contract.MethodNetworkInterfaces {
				t.Fatalf("daemon calls before abort = %v", methods)
			}
		})
	}
}

func TestExplicitBindingAndNoninteractiveCreateSkipNetworkQuery(t *testing.T) {
	for _, test := range []struct {
		name    string
		options configCreateOptions
		console bool
	}{
		{
			name: "explicit flags on console",
			options: configCreateOptions{
				binding: bindingFlags{interfaceSet: true, addressSet: true, interfaceID: "loopback", localIPv4: "127.0.0.1"},
				id:      "campus", profile: "jlu", username: "user", passwordStdin: true,
			}, console: true,
		},
		{
			name:    "noninteractive defaults",
			options: configCreateOptions{id: "campus", profile: "jlu", username: "user", passwordStdin: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var methods []string
			connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
				methods = append(methods, method)
				if method == contract.MethodConfigurationList {
					list, _ := contract.MarshalConfigurationListResult(contract.ConfigurationListResult{StorageProtection: "protected", Configurations: []contract.ConfigurationResult{}})
					return contract.NewSuccessResponse("1", list), nil
				}
				if method != contract.MethodConfigurationCreate {
					t.Fatalf("network query or unexpected method called: %q", method)
				}
				var request contract.ConfigurationCreatePayload
				if err := json.Unmarshal(payload, &request); err != nil {
					t.Fatal(err)
				}
				if request.NetworkBindingPolicy.Mode == "" {
					t.Fatal("create omitted network binding policy")
				}
				result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
					NetworkBindingPolicy: request.NetworkBindingPolicy, ConfigurationID: "campus",
					InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom", Username: "user",
					CredentialStored: true, StorageProtection: "protected",
				})
				return contract.NewSuccessResponse("1", result), nil
			}}
			deps := hotAuthDependencies(t, connection)
			deps.stdin = strings.NewReader("\n\npassword\n")
			deps.readStdinPassword = readPasswordStdin
			deps.inputIsConsole = func(io.Reader) bool { return test.console }
			deps.readInteractivePassword = func(io.Reader, io.Writer) (string, error) { return "password", nil }
			if err := runConfigCreate(test.options, deps); err != nil {
				t.Fatal(err)
			}
			if test.console {
				if len(methods) != 2 || methods[0] != contract.MethodConfigurationList || methods[1] != contract.MethodConfigurationCreate {
					t.Fatalf("interactive daemon calls = %v", methods)
				}
			} else if len(methods) != 1 || methods[0] != contract.MethodConfigurationCreate {
				t.Fatalf("noninteractive daemon calls = %v", methods)
			}
		})
	}
}

func TestInteractiveCreateSubmitsSelectedBindingAndFollowingPassword(t *testing.T) {
	observedAt := "2026-10-07T00:00:00Z"
	network, err := contract.MarshalNetworkInterfacesResult(contract.NetworkInterfacesResult{
		Available: true, Revision: 1, ObservedAt: &observedAt,
		Interfaces: []contract.NetworkInterfaceResult{{
			InterfaceID: "software-loopback", DisplayName: "Loopback", OperationalState: "up",
			PhysicalMedium: "unknown", AddressAssignmentMethod: "unknown",
			IPv4Assignments: []contract.NetworkIPv4Assignment{{
				Address: "127.0.0.1", PrefixLength: 8, ExplicitBindable: true,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := contract.MarshalConfigurationListResult(contract.ConfigurationListResult{
		StorageProtection: "protected", Configurations: []contract.ConfigurationResult{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	var create contract.ConfigurationCreatePayload
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		methods = append(methods, method)
		switch method {
		case contract.MethodNetworkInterfaces:
			return contract.NewSuccessResponse("1", network), nil
		case contract.MethodConfigurationList:
			return contract.NewSuccessResponse("1", list), nil
		case contract.MethodConfigurationCreate:
			if err := json.Unmarshal(payload, &create); err != nil {
				t.Fatal(err)
			}
			result, err := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
				NetworkBindingPolicy: create.NetworkBindingPolicy, ConfigurationID: create.ConfigurationID,
				InstitutionDisplayName: "吉林大学", InstitutionProfileID: create.InstitutionProfileID, AuthenticationProtocolID: "drcom",
				Username: create.Username, CredentialStored: true, StorageProtection: "protected",
				AutoLogin: create.AutoLogin, AutoReconnect: create.AutoReconnect,
			})
			if err != nil {
				t.Fatal(err)
			}
			return contract.NewSuccessResponse("1", result), nil
		default:
			t.Fatalf("unexpected daemon method %q", method)
			return contract.Response{}, nil
		}
	}}
	deps := hotAuthDependencies(t, connection)
	deps.stdin = strings.NewReader("1\nchosen-password\n")
	deps.inputIsConsole = func(io.Reader) bool { return true }
	deps.readInteractivePassword = func(input io.Reader, _ io.Writer) (string, error) {
		return readPasswordLine(input)
	}
	if err := runConfigCreate(configCreateOptions{
		id: "campus", profile: "jlu", username: "user",
		autoLoginExplicit: true, autoReconnectExplicit: true,
	}, deps); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(methods, "|"); got != "network.interfaces|configuration.list|configuration.create" {
		t.Fatalf("daemon calls = %q", got)
	}
	if create.NetworkBindingPolicy.Mode != "explicit_interface_and_local_ipv4" ||
		create.NetworkBindingPolicy.InterfaceID != "software-loopback" ||
		create.NetworkBindingPolicy.LocalIPv4Address != "127.0.0.1" || create.Password != "chosen-password" {
		t.Fatalf("create payload did not preserve selection and following password: %#v", create)
	}
}

func TestConfigCreateNonInteractiveSkipsPrompts(t *testing.T) {
	var capturedPayload json.RawMessage
	connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		capturedPayload = append(json.RawMessage(nil), payload...)
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
			NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
			ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
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
		result, _ := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
			NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
			ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
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

func TestConfigOverrideCreateReplaceAndClearPayloads(t *testing.T) {
	result, err := contract.MarshalConfigurationResult(contract.ConfigurationResult{RuntimeAvailability: "available",
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: automaticNetworkBindingPolicy},
		ConfigurationID:      "campus", InstitutionDisplayName: "吉林大学", InstitutionProfileID: "jlu", AuthenticationProtocolID: "drcom",
		Username: "user", CredentialStored: true, StorageProtection: "protected",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		method string
		call   func(authDependencies) error
		want   string
	}{
		{
			name:   "create replacement",
			method: contract.MethodConfigurationCreate,
			call: func(deps authDependencies) error {
				return runConfigCreate(configCreateOptions{id: "campus", profile: "jlu", username: "user", passwordStdin: true,
					protocolOverrideFile: writeOverrideFixture(t, `{"x":"✓"}`), protocolOverrideFileSet: true}, deps)
			},
			want: `{"x":"✓"}`,
		},
		{
			name:   "update replacement",
			method: contract.MethodConfigurationUpdate,
			call: func(deps authDependencies) error {
				return runConfigUpdate(configUpdateOptions{id: "campus", protocolOverrideFile: writeOverrideFixture(t, `{"x":"✓"}`), protocolOverrideFileSet: true}, deps)
			},
			want: `{"x":"✓"}`,
		},
		{
			name:   "update clear",
			method: contract.MethodConfigurationUpdate,
			call: func(deps authDependencies) error {
				return runConfigUpdate(configUpdateOptions{id: "campus", clearProtocolOverride: true}, deps)
			},
			want: "null",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var wire json.RawMessage
			connection := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
				if method != test.method {
					t.Fatalf("method = %q, want %q", method, test.method)
				}
				wire = append(json.RawMessage(nil), payload...)
				return contract.NewSuccessResponse("1", result), nil
			}}
			deps := hotAuthDependencies(t, connection)
			deps.stdin = strings.NewReader("password\n")
			deps.readStdinPassword = readPasswordStdin
			deps.inputIsConsole = func(io.Reader) bool { return false }
			if err := test.call(deps); err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(wire, &payload); err != nil {
				t.Fatal(err)
			}
			if got := string(payload["protocolContextOverride"]); got != test.want {
				t.Fatalf("protocolContextOverride = %s, want %s; wire=%s", got, test.want, wire)
			}
		})
	}
}

func writeOverrideFixture(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/override.json"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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

func TestCLIConfigurationMissingProfileAndAvailabilityStrictness(t *testing.T) {
	raw := []byte(`{"configurationId":"saved","displayName":"","institutionProfileId":"missing","institutionDisplayName":"","authenticationProtocolId":"","username":"u","credentialStored":true,"storageProtection":"protected","autoLogin":false,"autoReconnect":false,"networkBindingPolicy":{"mode":"automatically_select_latest_available"},"runtimeAvailability":"profile_unavailable"}`)
	row, err := decodeConfiguration(raw)
	if err != nil || row.RuntimeAvailability != "profile_unavailable" || row.InstitutionDisplayName != "" {
		t.Fatal("CLI rejected manageable row", err)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"runtimeAvailability":"profile_unavailable"`, `"runtimeAvailability":"available"`, 1), strings.Replace(string(raw), `"runtimeAvailability":"profile_unavailable"`, `"runtimeAvailability":"future"`, 1), strings.Replace(string(raw), `"runtimeAvailability":`, `"privateAvailability":`, 1), strings.Replace(string(raw), `"username":"u"`, `"username":"u","\u0075sername":"u"`, 1)} {
		if _, err := decodeConfiguration([]byte(bad)); err == nil {
			t.Fatal("CLI accepted inconsistent row")
		}
	}
}
