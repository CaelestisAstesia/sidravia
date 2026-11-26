package cli

import (
	"bytes"
	"strings"
	"testing"

	"sidravia/internal/cli/internal/configcmd"
)

func TestConfigCreateRejectsPasswordOnArgvShape(t *testing.T) {
	deps := commandDependencies{
		configList: func() error { return nil }, configShow: func(string) error { return nil },
		configCreate: func(configcmd.CreateOptions) error { return nil }, configUpdate: func(configcmd.UpdateOptions) error { return nil },
		configSetPassword: func(configcmd.PasswordOptions) error { return nil }, configRemove: func(string, bool, bool) error { return nil },
		output: &bytes.Buffer{},
	}
	if err := runCommand([]string{"config", "create", "--id", "campus", "--profile", "jlu", "--username", "user", "--password", "secret"}, deps); err == nil || !strings.HasPrefix(err.Error(), "用法错误，请运行 sidraviactl help config create") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateAndRemoveExposeExplicitInsecureStorageConsent(t *testing.T) {
	var update configcmd.UpdateOptions
	var removeConsent bool
	deps := commandDependencies{output: &bytes.Buffer{}, configUpdate: func(o configcmd.UpdateOptions) error { update = o; return nil }, configRemove: func(_ string, _ bool, allow bool) error { removeConsent = allow; return nil }}
	if err := runCommand([]string{"config", "update", "campus", "--username", "new", "--allow-insecure-storage"}, deps); err != nil {
		t.Fatal(err)
	}
	if !update.AllowInsecure || update.Username == nil || *update.Username != "new" {
		t.Fatal("update consent lost")
	}
	if err := runCommand([]string{"config", "remove", "campus", "--yes", "--allow-insecure-storage"}, deps); err != nil {
		t.Fatal(err)
	}
	if !removeConsent {
		t.Fatal("remove consent lost")
	}
}
