package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
)

type reconnectTestFactory struct {
	appTestProtocolFactory
	runs chan *reconnectTestRun
}

func (factory *reconnectTestFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	run := &reconnectTestRun{failure: make(chan *protocol.AuthenticationProtocolRunFailure, 1)}
	factory.runs <- run
	return run, nil
}

type reconnectTestRun struct {
	failure chan *protocol.AuthenticationProtocolRunFailure
}

func (run *reconnectTestRun) Execute(ctx context.Context, _ protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	select {
	case failure := <-run.failure:
		return failure
	case <-ctx.Done():
		return nil
	}
}

type reconnectTestRetryPolicy struct{}

func (reconnectTestRetryPolicy) Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool) {
	return time.Second, true
}

type reconnectTestScheduler struct{ scheduled chan func() }

func (scheduler reconnectTestScheduler) Schedule(_ time.Duration, callback func()) session.RetryCancellation {
	scheduler.scheduled <- callback
	return appNoOpRetryCancellation{}
}

func TestReconnectPreferenceAppliesOnlyToNextManualSession(t *testing.T) {
	for _, oldPolicy := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[oldPolicy], func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			ctx := context.Background()

			factory := &reconnectTestFactory{
				appTestProtocolFactory: appTestProtocolFactory{id: "drcom"},
				runs:                   make(chan *reconnectTestRun, 4),
			}
			registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
			if err != nil {
				t.Fatal(err)
			}
			setup.application.authenticationResolver.protocols = registry
			scheduler := reconnectTestScheduler{scheduled: make(chan func(), 4)}
			customSupervisor := supervisor.New(supervisor.Dependencies{
				Now:            func() time.Time { return time.Unix(100, 0) },
				RetryPolicy:    reconnectTestRetryPolicy{},
				RetryScheduler: scheduler,
			})
			if err := setup.supervisor.Close(); err != nil {
				t.Fatal(err)
			}
			setup.supervisor.Wait()
			setup.supervisor = customSupervisor
			setup.application, err = NewApplication(setup.catalog, setup.application.profiles, setup.application.authenticationResolver, customSupervisor)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := setup.catalog.Update(ctx, "configuration-1", config.Update{AutoReconnect: &oldPolicy}); err != nil {
				t.Fatal(err)
			}
			if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
				t.Fatal(err)
			}
			started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil {
				t.Fatal(err)
			}
			var run *reconnectTestRun
			select {
			case run = <-factory.runs:
			case <-time.After(time.Second):
				t.Fatal("protocol run was not created")
			}

			newPolicy := !oldPolicy
			if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoReconnect: &newPolicy}); err != nil {
				t.Fatal(err)
			}
			retained, err := setup.application.GetSession(ctx, started.SessionID)
			if err != nil || retained.AuthenticationSessionID != started.SessionID {
				t.Fatalf("preference edit replaced current Session: %#v, %v", retained, err)
			}

			run.failure <- &protocol.AuthenticationProtocolRunFailure{
				Code:                   "network_timeout",
				HandlingRecommendation: protocol.RetryAfterStandardDelay,
			}
			want := session.BlockedByError
			if oldPolicy {
				want = session.WaitingBeforeRetry
			}
			waitForApplicationSessionState(t, setup.application, started.SessionID, want)
			if oldPolicy {
				select {
				case <-scheduler.scheduled:
				case <-time.After(time.Second):
					t.Fatal("current Session did not retain its enabled retry policy")
				}
			} else {
				select {
				case <-scheduler.scheduled:
					t.Fatal("current Session adopted the newly enabled retry policy early")
				case <-time.After(20 * time.Millisecond):
				}
			}

			restarted, err := setup.application.RestartSession(ctx, started.SessionID)
			if err != nil || restarted.AuthenticationSessionID == started.SessionID {
				t.Fatalf("manual restart did not create a new Session: %#v, %v", restarted, err)
			}
			var replacementRun *reconnectTestRun
			select {
			case replacementRun = <-factory.runs:
			case <-time.After(time.Second):
				t.Fatal("replacement protocol run was not created")
			}
			replacementRun.failure <- &protocol.AuthenticationProtocolRunFailure{
				Code:                   "network_timeout",
				HandlingRecommendation: protocol.RetryAfterStandardDelay,
			}
			replacementWant := session.BlockedByError
			if newPolicy {
				replacementWant = session.WaitingBeforeRetry
			}
			waitForApplicationSessionState(t, setup.application, restarted.AuthenticationSessionID, replacementWant)
		})
	}
}

func TestReconnectReplacementCleanupFailureKeepsOldSessionQuarantined(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	latest := true
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoReconnect: &latest}); err != nil {
		t.Fatal(err)
	}
	if err := setup.supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	err = func() error {
		_, err := setup.application.RestartSession(ctx, started.SessionID)
		return err
	}()
	if !errors.Is(err, ErrConfigurationSessionInvalidation) || !errors.Is(err, session.ErrAuthenticationSessionClosed) {
		t.Fatalf("replacement cleanup error lost its causes: %v", err)
	}
	if _, err := setup.application.EnsureSessionRunning(ctx, started.SessionID); !errors.Is(err, supervisor.ErrSessionStateConflict) {
		t.Fatalf("quarantined Session can still resume: %v", err)
	}
	if _, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1"); !errors.Is(err, ErrConfigurationSessionInvalidation) {
		t.Fatalf("failed cleanup association did not remain recoverable: %v", err)
	}
}

func TestReconnectPreferenceResumePathsAndToggleBack(t *testing.T) {
	for _, path := range []string{"configuration-start", "session-ensure"} {
		t.Run(path, func(t *testing.T) {
			setup := newApplicationTestSetup(t)
			defer setup.cleanup()
			ctx := context.Background()
			started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
			if err != nil {
				t.Fatal(err)
			}
			latest := true
			if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoReconnect: &latest}); err != nil {
				t.Fatal(err)
			}
			if _, err := setup.application.StopSession(ctx, started.SessionID); err != nil {
				t.Fatal(err)
			}
			waitForApplicationSessionState(t, setup.application, started.SessionID, session.Suspended)

			var replacement session.AuthenticationSessionID
			if path == "configuration-start" {
				result, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
				if err != nil {
					t.Fatal(err)
				}
				replacement = result.SessionID
				if result.Outcome != SessionStartCreated {
					t.Fatalf("replacement outcome = %q", result.Outcome)
				}
			} else {
				result, err := setup.application.EnsureSessionRunning(ctx, started.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				replacement = result.SessionID
				if result.Outcome != SessionStartCreated {
					t.Fatalf("replacement outcome = %q", result.Outcome)
				}
			}
			if replacement == started.SessionID {
				t.Fatal("manual resume kept a Session with an obsolete reconnect policy")
			}
		})
	}

	t.Run("toggle-back-reuses-session", func(t *testing.T) {
		setup := newApplicationTestSetup(t)
		defer setup.cleanup()
		ctx := context.Background()
		started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
		if err != nil {
			t.Fatal(err)
		}
		changed, original := true, false
		for _, value := range []bool{changed, original} {
			if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoReconnect: &value}); err != nil {
				t.Fatal(err)
			}
		}
		restarted, err := setup.application.RestartSession(ctx, started.SessionID)
		if err != nil || restarted.AuthenticationSessionID != started.SessionID {
			t.Fatalf("toggle-back restarted immutable runtime: %#v, %v", restarted, err)
		}
	})

	t.Run("one-shot-is-not-associated", func(t *testing.T) {
		setup := newApplicationTestSetup(t)
		defer setup.cleanup()
		ctx := context.Background()
		started, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
		if err != nil {
			t.Fatal(err)
		}
		latest := true
		if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{AutoReconnect: &latest}); err != nil {
			t.Fatal(err)
		}
		restarted, err := setup.application.RestartSession(ctx, started.SessionID)
		if err != nil || restarted.AuthenticationSessionID != started.SessionID {
			t.Fatalf("configuration preference replaced one-shot Session: %#v, %v", restarted, err)
		}
	})
}
