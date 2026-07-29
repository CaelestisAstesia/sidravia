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
