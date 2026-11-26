package session

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"sidravia/internal/connection/auth/protocol"
	environment "sidravia/internal/connection/environment"
	profilevalue "sidravia/internal/connection/profile"
)

type controlledFactory struct {
	mu               sync.Mutex
	inputs           []protocol.AuthenticationProtocolRunCreationInputs
	runs             []*controlledRun
	holdCancellation bool
	creationError    error
}

func (factory *controlledFactory) ProtocolID() profilevalue.ProtocolID {
	return "test-protocol"
}

func (factory *controlledFactory) ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error {
	return nil
}

func (factory *controlledFactory) ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error {
	return nil
}

func (factory *controlledFactory) CreateAuthenticationProtocolRun(inputs protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	factory.inputs = append(factory.inputs, inputs)
	if factory.creationError != nil {
		return nil, factory.creationError
	}
	run := newControlledRun(factory.holdCancellation)
	factory.runs = append(factory.runs, run)
	return run, nil
}

func (factory *controlledFactory) creationInputs() []protocol.AuthenticationProtocolRunCreationInputs {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return append([]protocol.AuthenticationProtocolRunCreationInputs(nil), factory.inputs...)
}

func (factory *controlledFactory) run(index int) *controlledRun {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.runs[index]
}

type controlledRun struct {
	started          chan protocol.AuthenticationProtocolRunObserver
	executing        chan struct{}
	canceled         chan error
	release          chan *protocol.AuthenticationProtocolRunFailure
	holdCancellation bool
	once             sync.Once
	releaseDeadline  context.Context
	releaseCancel    context.CancelFunc
}

func newControlledRun(holdCancellation bool) *controlledRun {
	releaseDeadline, releaseCancel := context.WithTimeout(context.Background(), time.Second)
	return &controlledRun{
		started:          make(chan protocol.AuthenticationProtocolRunObserver, 1),
		executing:        make(chan struct{}, 1),
		canceled:         make(chan error, 1),
		release:          make(chan *protocol.AuthenticationProtocolRunFailure, 1),
		holdCancellation: holdCancellation,
		releaseDeadline:  releaseDeadline,
		releaseCancel:    releaseCancel,
	}
}

func (run *controlledRun) Execute(ctx context.Context, observer protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	defer run.releaseCancel()
	select {
	case run.started <- observer:
	case <-run.releaseDeadline.Done():
		return nil
	}
	select {
	case run.executing <- struct{}{}:
	default:
	}
	select {
	case failure := <-run.release:
		return failure
	case <-ctx.Done():
		run.canceled <- context.Cause(ctx)
		if !run.holdCancellation {
			return nil
		}
		select {
		case failure := <-run.release:
			return failure
		case <-run.releaseDeadline.Done():
			return nil
		}
	}
}

func (run *controlledRun) establish(ctx context.Context) error {
	select {
	case observer := <-run.started:
		observer.AuthenticationEstablished()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (run *controlledRun) waitForStart(ctx context.Context) error {
	select {
	case <-run.executing:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (run *controlledRun) unblock(failure *protocol.AuthenticationProtocolRunFailure) {
	run.once.Do(func() { run.release <- failure })
}

func (run *controlledRun) waitForCancellation(ctx context.Context) error {
	select {
	case cause := <-run.canceled:
		return cause
	case <-ctx.Done():
		return ctx.Err()
	}
}

var errControlledFactoryCreation = errors.New("controlled factory creation failed")

type unavailableRetryPolicy struct{}

func (unavailableRetryPolicy) Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool) {
	return 0, false
}

type noOpRetryScheduler struct{}

func (noOpRetryScheduler) Schedule(time.Duration, func()) RetryCancellation {
	return noOpRetryCancellation{}
}

type noOpRetryCancellation struct{}

func (noOpRetryCancellation) Cancel() {}

func testDependencies(now func() time.Time) Dependencies {
	return Dependencies{Now: now, RetryPolicy: unavailableRetryPolicy{}, RetryScheduler: noOpRetryScheduler{}}
}

func (*controlledFactory) ValidateUsername(string) error { return nil }

// Existing lifecycle tests use an already observed standby to isolate cleanup
// ordering from candidate stabilization. Stabilization has dedicated clock tests.
func withStandby(t *testing.T, network environment.Snapshot, id string) environment.Snapshot {
	t.Helper()
	interfaces := network.Interfaces()
	interfaces = append(interfaces, usableSystemNetworkSnapshot(t, network.Revision, id, id).Interfaces()...)
	return environment.NewSnapshot(network.Revision, network.ObservedAt, interfaces)
}

func fireAutomaticWake(t *testing.T, session *AuthenticationSession) {
	t.Helper()
	scheduler, ok := session.retryScheduler.(*manualRetryScheduler)
	if !ok {
		t.Fatal("test requires manual scheduler")
	}
	ctx := testContext(t)
	for {
		_ = sessionSnapshot(t, ctx, session)
		if n := scheduler.count(); n > 0 && !scheduler.cancellation(n-1).cancelled() {
			scheduler.callback(n - 1)()
			_ = sessionSnapshot(t, ctx, session)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("no pending automatic wake")
		case <-time.After(time.Millisecond):
		}
	}
}

func newTestSession(t *testing.T, factory *controlledFactory, intent Intent) *AuthenticationSession {
	t.Helper()
	definition := validRuntimeDefinition(t)
	definition.AuthenticationProtocolFactory = factory
	scheduler := &manualRetryScheduler{}
	dependencies := testDependencies(scheduler.bindClock(func() time.Time { return time.Unix(100, 0) }))
	dependencies.RetryScheduler = scheduler
	session, err := NewAuthenticationSession(definition, intent, dependencies)
	if err != nil {
		t.Fatalf("NewAuthenticationSession() error = %v", err)
	}
	session.Start()
	return session
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func shutdownTestSession(t *testing.T, session *AuthenticationSession) {
	t.Helper()
	ctx := testContext(t)
	if err := session.Shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v", err)
	}
}

func sessionSnapshot(t *testing.T, ctx context.Context, session *AuthenticationSession) Snapshot {
	t.Helper()
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot() error = %v", err)
	}
	return snapshot
}

func waitForState(t *testing.T, ctx context.Context, session *AuthenticationSession, want State) Snapshot {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		got := sessionSnapshot(t, ctx, session)
		if got.State == want {
			return got
		}
		select {
		case <-ctx.Done():
			t.Fatalf("state = %q, want %q: %v", got.State, want, ctx.Err())
		case <-tick.C:
		}
	}
}

func waitForFactoryRun(t *testing.T, ctx context.Context, factory *controlledFactory, index int) *controlledRun {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		inputs := factory.creationInputs()
		if len(inputs) > index {
			return factory.run(index)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("factory did not create run %d: %v", index, ctx.Err())
		case <-tick.C:
		}
	}
}

func usableSystemNetworkSnapshot(t *testing.T, revision uint64, interfaceID, displayName string) environment.Snapshot {
	t.Helper()
	networkInterface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:              environment.InterfaceID(interfaceID),
		DisplayName:              displayName,
		OperationalState:         environment.OperationalStateUp,
		PhysicalMedium:           environment.PhysicalMediumWired,
		HardwareBacked:           true,
		PhysicalConnectorPresent: true,
		AddressAssignmentMethod:  environment.AddressAssignmentDHCP,
		HardwareAddress:          []byte{0, 1, 2, 3, 4, 5},
		IPv4AddressAssignments: []environment.IPv4AddressAssignment{{
			Address: netip.MustParseAddr("192.0.2.10"), PrefixLength: 24,
		}},
	})
	if err != nil {
		t.Fatalf("NewNetworkInterface() error = %v", err)
	}
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{networkInterface})
}

var _ protocol.AuthenticationProtocolRun = (*controlledRun)(nil)
