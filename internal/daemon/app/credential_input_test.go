package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	connectionconfig "sidravia/internal/connection/config"
	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
)

func TestInvalidNewCredentialsDoNotCommitOrRetireActiveSession(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	factory := usernameTestFactory(t, setup)
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, started.SessionID, session.Authenticated)
	<-factory.credentials
	original := usernameDisk(setup)
	for _, bad := range []string{" user", "user ", "a b", "帐号", "ａｂ！", "pass\tword", "pass\x7fword"} {
		for _, password := range []bool{false, true} {
			update := config.Update{}
			if password {
				update.Password = &bad
			} else {
				update.Username = &bad
			}
			_, err := setup.application.UpdateConfiguration(ctx, "configuration-1", update)
			requireUsernameRejection(t, err)
			if strings.Contains(err.Error(), bad) {
				t.Fatal("credential leaked in validation error")
			}
			value := appTestConfiguration("new-config")
			value.Username = "valid-user"
			secret := "valid-password"
			if password {
				secret = bad
			} else {
				value.Username = bad
			}
			_, err = setup.application.CreateConfiguration(ctx, value, secret, false)
			requireUsernameRejection(t, err)
			if !reflect.DeepEqual(original, usernameDisk(setup)) {
				t.Fatal("invalid input wrote data")
			}
		}
	}
	if _, err := setup.application.GetSession(ctx, started.SessionID); err != nil {
		t.Fatal("old session removed")
	}
	select {
	case <-factory.canceled:
		t.Fatal("invalid edit canceled active run")
	default:
	}
}

func TestStoredLegacyCredentialsAreNotNewInput(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	legacyUser, legacyPassword := "旧账号 ", "旧 密码 "
	if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{Username: &legacyUser}, Password: &legacyPassword}); err != nil {
		t.Fatal(err)
	}
	before := usernameDisk(setup)
	if _, _, err := setup.application.ListConfigurations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.application.GetConfiguration(ctx, "configuration-1"); err != nil {
		t.Fatal(err)
	}
	resolved, err := setup.application.authenticationResolver.Resolve(ctx, "configuration-1", "legacy")
	if err != nil || resolved.AuthenticationCredential.Username != legacyUser || resolved.AuthenticationCredential.Password != legacyPassword {
		t.Fatalf("legacy credential changed: %v", err)
	}
	if !reflect.DeepEqual(before, usernameDisk(setup)) {
		t.Fatal("reading legacy input rewrote data")
	}
	label := "new label"
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{DisplayName: &label, Username: &legacyUser}}); err != nil {
		t.Fatal(err)
	}
	_, credential, err := setup.catalog.Resolve(ctx, "configuration-1")
	if err != nil || credential.Username != legacyUser || credential.Password != legacyPassword {
		t.Fatal("metadata edit altered old credentials")
	}
	for _, bad := range []string{"new user", "密码", "pass word"} {
		_, err := setup.application.authenticationResolver.ResolveOneShot(ctx, OneShotAuthenticationInput{AuthenticationCredential: credential}, "new")
		requireUsernameRejection(t, err)
		credential.Username = "valid-user"
		credential.Password = bad
		_, err = setup.application.authenticationResolver.ResolveOneShot(ctx, OneShotAuthenticationInput{AuthenticationCredential: credential}, "new")
		requireUsernameRejection(t, err)
	}
}
