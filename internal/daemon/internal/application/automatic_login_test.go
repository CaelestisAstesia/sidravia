package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	connectionconfig "sidravia/internal/connection/config"
	config "sidravia/internal/daemon/internal/application/internal/configstore"
	"sidravia/internal/document"
)

func TestPerformAutomaticLoginNoConfigurationReturnsNil(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	if err := setup.application.PerformAutomaticLogin(ctx); err != nil {
		t.Fatalf("PerformAutomaticLogin() error = %v", err)
	}
	if sessions, err := setup.application.ListSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatal("no automatic configuration must not create a Session")
	}
}

func TestPerformAutomaticLoginStartsAutoLoginConfiguration(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	enable := true
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &enable}}); err != nil {
		t.Fatal(err)
	}
	if err := setup.application.PerformAutomaticLogin(ctx); err != nil {
		t.Fatalf("PerformAutomaticLogin() error = %v", err)
	}
	setup.application.sessionIndexMu.Lock()
	sessionID := setup.application.sessionsByConfig["configuration-1"]
	setup.application.sessionIndexMu.Unlock()
	if sessionID == "" {
		t.Fatal("auto-login did not start a Session")
	}
}

func TestPerformAutomaticLoginFailureReturnsSafeError(t *testing.T) {
	setup := newApplicationTestSetup(t)
	ctx := context.Background()
	enable := true
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &enable}}); err != nil {
		t.Fatal(err)
	}
	if err := setup.supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	setup.supervisor.Wait()
	err := setup.application.PerformAutomaticLogin(ctx)
	if err == nil {
		t.Fatal("PerformAutomaticLogin() unexpectedly succeeded")
	}
	if err.Error() != "automatic_login_failed" {
		t.Fatalf("error = %q, want %q", err.Error(), "automatic_login_failed")
	}
	var failure *automaticLoginFailure
	if !errors.As(err, &failure) || failure.Unwrap() == nil || failure.Unwrap().Error() != "supervisor is closed" {
		t.Fatal("automatic login lost closed supervisor cause")
	}
}

// automaticSelectionContext pauses catalog selection without a production hook.
// The edit's validation callback reports whether it passed the app operation gate.
type automaticSelectionContext struct {
	context.Context
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (c *automaticSelectionContext) Err() error {
	if c.calls.Add(1) == 2 {
		close(c.entered)
		select {
		case <-c.release:
		case <-c.Context.Done():
		}
	}
	return c.Context.Err()
}

type editAdmissionContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *editAdmissionContext) Err() error {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Err()
}
func TestAutomaticLoginSerializesSelection(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	enable := true
	if _, e := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &enable}}); e != nil {
		t.Fatal(e)
	}
	autoctx := &automaticSelectionContext{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
	autodone := make(chan error, 1)
	go func() { autodone <- setup.application.PerformAutomaticLogin(autoctx) }()
	select {
	case <-autoctx.entered:
	case <-ctx.Done():
		t.Fatal("automatic selection did not begin")
	}
	editctx := &editAdmissionContext{Context: ctx, entered: make(chan struct{})}
	editdone := make(chan error, 1)
	disable, user, password := false, "new-user", "new-password"
	go func() {
		_, e := setup.application.UpdateConfiguration(editctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &disable, Username: &user}, Password: &password})
		editdone <- e
	}()
	crossed := false
	select {
	case <-editctx.entered:
		crossed = true
	case <-time.After(100 * time.Millisecond):
	}
	close(autoctx.release)
	if e := <-autodone; e != nil {
		t.Fatal(e)
	}
	if e := <-editdone; e != nil {
		t.Fatal(e)
	}
	sessions, e := setup.application.ListSessions(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if crossed {
		t.Error("edit entered while automatic selection was in progress")
	}
	if len(sessions) != 0 {
		t.Fatal("disabled configuration retained automatic Session after credential edit")
	}
	_, cred, e := setup.catalog.Resolve(ctx, "configuration-1")
	if e != nil || cred.Username != user || cred.Password != password {
		t.Fatal("edit did not commit current credentials")
	}
}
func TestAutomaticLoginUsesCommittedChoice(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	on, off := true, false
	if _, e := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &on}}); e != nil {
		t.Fatal(e)
	}
	user, password := "new-user", "new-password"
	if _, e := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &off, Username: &user}, Password: &password}); e != nil {
		t.Fatal(e)
	}
	if e := setup.application.PerformAutomaticLogin(ctx); e != nil {
		t.Fatal(e)
	}
	sessions, e := setup.application.ListSessions(ctx)
	if e != nil || len(sessions) != 0 {
		t.Fatal("committed disable started a Session")
	}
	b := appTestConfiguration("configuration-b")
	b.AutoLogin = true
	if _, e := setup.application.CreateConfiguration(ctx, b, "b-password", false); e != nil {
		t.Fatal(e)
	}
	if e := setup.application.PerformAutomaticLogin(ctx); e != nil {
		t.Fatal(e)
	}
	sessions, e = setup.application.ListSessions(ctx)
	if e != nil || len(sessions) != 1 || sessions[0].ConfigurationID != "configuration-b" {
		t.Fatal("automatic login did not follow committed choice")
	}
}

// Err fails at an existing context boundary, without a production injection hook.
type automaticFailureContext struct {
	context.Context
	calls, failAt int
	cause         error
}

func (ctx *automaticFailureContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.failAt {
		return ctx.cause
	}
	return nil
}

func TestAutomaticLoginRetainsListAndStartCausesWithSafeText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		start  bool
	}{
		{"list", 1, false}, {"start resolution", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			if tc.start {
				enable := true
				if _, err := setup.application.UpdateConfiguration(context.Background(), "configuration-1", config.Update{Patch: connectionconfig.Patch{AutoLogin: &enable}}); err != nil {
					t.Fatal(err)
				}
			}
			cause := errors.New("account-marker password-marker /private/path cause-marker")
			ctx := &automaticFailureContext{Context: context.Background(), failAt: tc.failAt, cause: cause}
			err := setup.application.PerformAutomaticLogin(ctx)
			if err == nil || err.Error() != "automatic_login_failed" {
				t.Fatalf("unsafe failure: %v", err)
			}
			var safe *automaticLoginFailure
			var original *document.Failure
			if !errors.As(err, &safe) || !errors.As(err, &original) || original.DiagnosticCause() != cause {
				t.Fatal("original persistence failure/cause lost")
			}
			if tc.start {
				var resolution *ResolutionFailure
				if !errors.As(err, &resolution) || safe.Unwrap() != resolution || resolution.Unwrap() != original {
					t.Fatal("start return object was replaced")
				}
			} else if safe.Unwrap() != original {
				t.Fatal("list return object was replaced")
			}
			if sessions, err := setup.application.ListSessions(context.Background()); err != nil || len(sessions) != 0 {
				t.Fatal("failure created a Session")
			}
		})
	}
}

func TestAutomaticLoginFailureUnwrapPreservesSentinelIdentity(t *testing.T) {
	cause := errors.New("account-marker password-marker /private/path cause-marker")
	err := &automaticLoginFailure{cause: cause}
	if err.Unwrap() != cause || !errors.Is(err, cause) || err.Error() != "automatic_login_failed" {
		t.Fatal("safe wrapper lost identity or echoed cause")
	}
}
