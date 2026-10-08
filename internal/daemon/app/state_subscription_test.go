package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/contract"
)

func nextApplicationState(t *testing.T, stream contract.StateEventStream) contract.StateEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := stream.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestApplicationStateEmptyFullSessionAndOriginalTerminal(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	bootstrap, stream, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if bootstrap.Sessions.Sessions == nil || bootstrap.Sessions.CleanupRequiredSessionIDs == nil || len(bootstrap.Sessions.Sessions) != 0 || bootstrap.Network.Available || bootstrap.Network.Revision != 0 || bootstrap.Network.ObservedAt != nil || bootstrap.Network.Interfaces == nil {
		t.Fatal("no observation fabricated", bootstrap)
	}
	raw, err := setup.supervisor.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	started, err := setup.application.StartOneShotAuthentication(ctx, validOneShotInput())
	if err != nil {
		t.Fatal(err)
	}
	changed := nextApplicationState(t, stream)
	if changed.Method != contract.EventMethodSessionChanged || changed.SessionChanged == nil || changed.SessionChanged.CleanupRequired {
		t.Fatal("wrong changed shape")
	}
	pair, err := raw.Next(ctx)
	if err != nil || pair.Kind != supervisor.StateSessionChanged || !reflect.DeepEqual(changed.SessionChanged.Session, toSessionResult(pair.Snapshot)) {
		t.Fatal("original pair not preserved", err)
	}
	full, sibling, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sibling.Close()
	actor, err := setup.application.GetSession(ctx, started.SessionID)
	if err != nil || len(full.Sessions.Sessions) != 1 || !reflect.DeepEqual(full.Sessions.Sessions[0], toSessionResult(actor)) || full.Sessions.Sessions[0].ProtocolSocket.State != "not_observed" || full.Sessions.Sessions[0].Revision == 0 {
		t.Fatal("full initial actor facts lost", err)
	}
	full.Sessions.Sessions[0].StateReason.Description = "consumer-mutated"
	changed.SessionChanged.Session.StateReason.Description = "consumer-mutated"
	fresh, third, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	if !reflect.DeepEqual(fresh.Sessions.Sessions[0], toSessionResult(actor)) {
		t.Fatal("bootstrap/event aliases actor")
	}
	if err := setup.application.RemoveSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	terminal := nextApplicationState(t, stream)
	final, err := raw.Next(ctx)
	if err != nil || final.Kind != supervisor.StateSessionRemoved || terminal.SessionRemoved == nil || terminal.SessionRemoved.SessionID != string(started.SessionID) || terminal.SessionRemoved.Revision != final.Revision || final.Revision < pair.Revision {
		t.Fatal("terminal invented final revision", terminal, err)
	}
	other := nextApplicationState(t, sibling)
	if !reflect.DeepEqual(terminal, other) {
		t.Fatal("sibling lost terminal")
	}
	canceled, cancel := context.WithCancelCause(ctx)
	cause := errors.New("single-next-cancel")
	cancel(cause)
	if _, err := stream.Next(canceled); !errors.Is(err, cause) {
		t.Fatal("custom cancellation lost", err)
	}
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	network := nextApplicationState(t, stream)
	if network.Method != contract.EventMethodNetworkChanged || network.NetworkChanged.Revision != 1 {
		t.Fatal("canceled Next closed owned stream or post-terminal Session event", network)
	}
	other = nextApplicationState(t, sibling)
	network.NetworkChanged.Interfaces[0].IPv4Assignments[0].Address = "192.0.2.99"
	*network.NetworkChanged.ObservedAt = "mutated"
	if other.NetworkChanged.Interfaces[0].IPv4Assignments[0].Address != "192.0.2.10" {
		t.Fatal("event siblings alias")
	}
	networkBootstrap, fourth, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fourth.Close()
	if len(networkBootstrap.Sessions.Sessions) != 0 || networkBootstrap.Network.Interfaces[0].IPv4Assignments[0].Address != "192.0.2.10" || !networkBootstrap.Network.Available {
		t.Fatal("network event aliases owner")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(ctx); !errors.Is(err, supervisor.ErrSubscriptionClosed) {
		t.Fatal("Close cause lost", err)
	}
}

// The second context check is inside opMu immediately before the first owner
// query. It executes real source mutations, without exposing private subs/maps.
type stateQueryContext struct {
	context.Context
	calls       atomic.Int32
	beforeQuery func()
}

func (ctx *stateQueryContext) Err() error {
	if ctx.calls.Add(1) == 2 {
		ctx.beforeQuery()
	}
	return ctx.Context.Err()
}

func TestApplicationStateRegistersBeforeFirstOwnerQuery(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	definition, err := setup.application.authenticationResolver.ResolveOneShot(ctx, validOneShotInput(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	var removed session.AuthenticationSessionID
	query := &stateQueryContext{Context: ctx, beforeQuery: func() {
		id, _, err := setup.supervisor.StartResolved(ctx, definition, session.SuspendAuthentication)
		if err != nil {
			t.Fatal(err)
		}
		removed = id
		if err := setup.supervisor.Remove(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
			t.Fatal(err)
		}
	}}
	bootstrap, stream, err := setup.application.SubscribeStateEvents(query)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(bootstrap.Sessions.Sessions) != 0 || !bootstrap.Network.Available {
		t.Fatal("first owner query did not see new facts")
	}
	event := nextApplicationState(t, stream)
	if event.SessionRemoved == nil || event.SessionRemoved.SessionID != string(removed) || event.SessionRemoved.Revision == 0 {
		t.Fatal("registration gap dropped removed resource")
	}
	event = nextApplicationState(t, stream)
	if event.NetworkChanged == nil || !reflect.DeepEqual(*event.NetworkChanged, bootstrap.Network) {
		t.Fatal("registration gap dropped accepted network")
	}
}

func TestApplicationStateContextAndProjectionDiscontinuity(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	if _, stream, err := setup.application.SubscribeStateEvents(nil); err == nil || stream != nil {
		t.Fatal("nil context accepted")
	}
	canceled, cancel := context.WithCancelCause(ctx)
	cause := errors.New("bootstrap-custom-cause")
	cancel(cause)
	if _, stream, err := setup.application.SubscribeStateEvents(canceled); !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || stream != nil {
		t.Fatal("bootstrap cancellation lost", err)
	}
	during, cancelDuring := context.WithCancelCause(ctx)
	query := &stateQueryContext{Context: during, beforeQuery: func() { cancelDuring(cause) }}
	if _, stream, err := setup.application.SubscribeStateEvents(query); !errors.Is(err, cause) || stream != nil {
		t.Fatal("post-registration cancellation transferred source", err)
	}
	_, stream, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(nil); err == nil {
		t.Fatal("nil Next accepted")
	}
	// Revision zero remains accepted by the original domain owner; the public
	// schema cannot represent available zero, so this stream must discontinue.
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(0, time.Unix(100, 0), nil)); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := setup.application.SubscribeStateEvents(ctx); !errors.Is(err, errStateProjection) || acquired != nil {
		t.Fatal("invalid bootstrap transferred source", err)
	}
	if _, err := stream.Next(ctx); !errors.Is(err, errStateProjection) {
		t.Fatal("unrepresentable source was fabricated/skipped", err)
	}
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(ctx); !errors.Is(err, errStateProjection) {
		t.Fatal("invalid source did not terminate ownership", err)
	}
	healthy, sibling, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil || healthy.Network.Revision != 1 {
		t.Fatal("projection failure changed original owner", err)
	}
	defer sibling.Close()
	if err := setup.supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sibling.Next(ctx); !errors.Is(err, supervisor.ErrSubscriptionShutdown) {
		t.Fatal("shutdown cause lost", err)
	}
	if _, stream, err := setup.application.SubscribeStateEvents(ctx); !errors.Is(err, supervisor.ErrSubscriptionShutdown) || stream != nil {
		t.Fatal("closed source acquired", err)
	}
}

func TestApplicationStateBootstrapBoundsDoNotTruncate(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	definition, err := setup.application.authenticationResolver.ResolveOneShot(ctx, validOneShotInput(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < contract.StateResourceCapacity-1; i++ {
		if _, _, err := setup.supervisor.StartResolved(ctx, definition, session.SuspendAuthentication); err != nil {
			t.Fatal(err)
		}
	}
	if _, stream, err := setup.application.SubscribeStateEvents(ctx); !errors.Is(err, errStateProjection) || !strings.Contains(err.Error(), "frame limit") || stream != nil {
		t.Fatal("full boundary truncated or wrongly counted", err)
	}
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := setup.supervisor.StartResolved(ctx, definition, session.SuspendAuthentication); err != nil {
		t.Fatal(err)
	}
	if _, stream, err := setup.application.SubscribeStateEvents(ctx); !errors.Is(err, errStateProjection) || !strings.Contains(err.Error(), "resource capacity") || stream != nil {
		t.Fatal("unavailable network resource excluded", err)
	}
	values, err := setup.application.ListSessionView(ctx)
	if err != nil || len(values.Sessions) != contract.StateResourceCapacity {
		t.Fatal("failed bootstrap mutated/truncated owner", err)
	}
}

func TestApplicationStateOverflowCauseRetainsOwner(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	_, stream, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	definition, err := setup.application.authenticationResolver.ResolveOneShot(ctx, validOneShotInput(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	// Each successful removal waits for the actor's closing producer and
	// forwarder. Thus all 256 distinct terminal resources are pending before
	// the network acceptance synchronously publishes resource 257.
	for i := 0; i < contract.StateResourceCapacity; i++ {
		id, _, err := setup.supervisor.StartResolved(ctx, definition, session.SuspendAuthentication)
		if err != nil {
			t.Fatal(err)
		}
		if err := setup.application.RemoveSession(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(ctx); !errors.Is(err, supervisor.ErrSubscriptionOverflow) {
		t.Fatal("overflow cause lost", err)
	}
	bootstrap, healthy, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil || len(bootstrap.Sessions.Sessions) != 0 || bootstrap.Network.Revision != 1 {
		t.Fatal("overflow changed state owner", err)
	}
	defer healthy.Close()
}

func TestApplicationStateOversizeEventDiscontinuesWithoutChangingOwner(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	_, stream, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	input := validOneShotInput()
	input.DisplayName = strings.Repeat("a", contract.StateFrameLimit)
	started, err := setup.application.StartOneShotAuthentication(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	query, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := stream.Next(query); !errors.Is(err, errStateProjection) || !strings.Contains(err.Error(), "frame limit") {
		t.Fatal("oversize event truncated/accepted", err)
	}
	if _, err := stream.Next(ctx); !errors.Is(err, errStateProjection) {
		t.Fatal("oversize source remained active", err)
	}
	actor, err := setup.application.GetSession(ctx, started.SessionID)
	if err != nil || actor.DisplayName != input.DisplayName {
		t.Fatal("event validation changed original owner", err)
	}
	if _, acquired, err := setup.application.SubscribeStateEvents(ctx); !errors.Is(err, errStateProjection) || acquired != nil {
		t.Fatal("oversize bootstrap transferred stream", err)
	}
	if err := setup.application.RemoveSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	bootstrap, healthy, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil || len(bootstrap.Sessions.Sessions) != 0 {
		t.Fatal("projection failure broke fresh bootstrap", err)
	}
	defer healthy.Close()
}

func TestApplicationStateRealCleanupFailureMetadata(t *testing.T) {
	// This additional real deadline case proves the new event and bootstrap
	// contract at a committed cleanup failure, beyond the existing list view.
	setup := newApplicationTestSetup(t)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); setup.cleanup() }()
	factory := &cleanupDeadlineFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}, canceled: make(chan struct{}), release: release}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	ctx := context.Background()
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, appTestNetworkSnapshot(t, 1)); err != nil {
		t.Fatal(err)
	}
	started, err := setup.application.StartConfigurationAuthentication(ctx, "configuration-1")
	if err != nil {
		t.Fatal(err)
	}
	waitForApplicationSessionState(t, setup.application, started.SessionID, session.Authenticated)
	_, stream, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	username := "durably-saved-new-user"
	if _, err := setup.application.UpdateConfiguration(ctx, "configuration-1", config.Update{Username: &username}); !errors.Is(err, ErrConfigurationSessionInvalidation) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("real cleanup failure missing", err)
	}
	changed := nextApplicationState(t, stream)
	if changed.SessionChanged == nil || !changed.SessionChanged.CleanupRequired || changed.SessionChanged.Session.State != "stopping" {
		t.Fatal("quarantine metadata lost", changed)
	}
	bootstrap, sibling, err := setup.application.SubscribeStateEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sibling.Close()
	if len(bootstrap.Sessions.CleanupRequiredSessionIDs) != 1 || bootstrap.Sessions.CleanupRequiredSessionIDs[0] != string(started.SessionID) || !reflect.DeepEqual(bootstrap.Sessions.Sessions[0], changed.SessionChanged.Session) {
		t.Fatal("cleanup fabricated actor revision or facts")
	}
	bootstrap.Sessions.CleanupRequiredSessionIDs[0] = "mutated"
	changed.SessionChanged.Session.DisplayName = "mutated"
	actor, err := setup.application.GetSession(ctx, started.SessionID)
	if err != nil || !reflect.DeepEqual(toSessionResult(actor), bootstrap.Sessions.Sessions[0]) {
		t.Fatal("cleanup consumer aliases actor", err)
	}
	unblock()
	if err := setup.application.RemoveSession(ctx, started.SessionID); err != nil {
		t.Fatal(err)
	}
	terminal := nextApplicationState(t, stream)
	if terminal.SessionRemoved == nil || terminal.SessionRemoved.SessionID != string(started.SessionID) {
		t.Fatal("cleanup completion lost terminal")
	}
}
