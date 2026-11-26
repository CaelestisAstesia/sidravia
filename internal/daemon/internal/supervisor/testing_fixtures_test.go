package supervisor

import (
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"sidravia/internal/connection/auth/protocol"
	credential "sidravia/internal/connection/credential"
	environment "sidravia/internal/connection/environment"
	profilevalue "sidravia/internal/connection/profile"
	"sidravia/internal/daemon/internal/session"
)

type stubFactory struct{}

func (stubFactory) ProtocolID() profilevalue.ProtocolID { return "test-protocol" }
func (stubFactory) ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error {
	return nil
}
func (stubFactory) ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error {
	return nil
}
func (stubFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return blockingRun{}, nil
}

type blockingRun struct{}

func (blockingRun) Execute(ctx context.Context, _ protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	<-ctx.Done()
	return nil
}

type heldCancellationFactory struct {
	run *heldCancellationRun
}

type sequentialHeldFactory struct {
	mu      sync.Mutex
	runs    []*heldCancellationRun
	created chan *heldCancellationRun
}

func (factory *sequentialHeldFactory) ProtocolID() profilevalue.ProtocolID {
	return "test-protocol"
}
func (factory *sequentialHeldFactory) ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error {
	return nil
}
func (factory *sequentialHeldFactory) ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error {
	return nil
}
func (factory *sequentialHeldFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	run := newHeldCancellationRun()
	factory.runs = append(factory.runs, run)
	if factory.created == nil {
		factory.created = make(chan *heldCancellationRun, 8)
	}
	factory.created <- run
	return run, nil
}
func (factory *sequentialHeldFactory) run(index int) *heldCancellationRun {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.runs) <= index {
		return nil
	}
	return factory.runs[index]
}

func (factory *sequentialHeldFactory) waitForRun(t *testing.T) *heldCancellationRun {
	t.Helper()
	factory.mu.Lock()
	if factory.created == nil {
		factory.created = make(chan *heldCancellationRun, 8)
	}
	created := factory.created
	factory.mu.Unlock()
	select {
	case run := <-created:
		return run
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for protocol Run creation")
		return nil
	}
}

func (factory *sequentialHeldFactory) releaseAll() {
	factory.mu.Lock()
	runs := append([]*heldCancellationRun(nil), factory.runs...)
	factory.mu.Unlock()
	for _, run := range runs {
		releaseHeldCancellationRun(run)
	}
}

func (heldCancellationFactory) ProtocolID() profilevalue.ProtocolID {
	return "test-protocol"
}

func (heldCancellationFactory) ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error {
	return nil
}

func (heldCancellationFactory) ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error {
	return nil
}

func (factory heldCancellationFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	return factory.run, nil
}

type heldCancellationRun struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

type stateRun struct {
	observer chan protocol.AuthenticationProtocolRunObserver
	finish   chan *protocol.AuthenticationProtocolRunFailure
	canceled chan struct{}
}

func newStateRun() *stateRun {
	return &stateRun{
		observer: make(chan protocol.AuthenticationProtocolRunObserver, 1),
		finish:   make(chan *protocol.AuthenticationProtocolRunFailure, 1),
		canceled: make(chan struct{}),
	}
}

func (run *stateRun) Execute(ctx context.Context, observer protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	run.observer <- observer
	select {
	case failure := <-run.finish:
		return failure
	case <-ctx.Done():
		close(run.canceled)
		return nil
	}
}

type stateFactory struct {
	mu      sync.Mutex
	runs    []*stateRun
	created chan *stateRun
}

func newStateFactory() *stateFactory                      { return &stateFactory{created: make(chan *stateRun, 4)} }
func (*stateFactory) ProtocolID() profilevalue.ProtocolID { return "test-protocol" }
func (*stateFactory) ValidateInstitutionProtocolConfiguration(profilevalue.ProtocolConfiguration) error {
	return nil
}
func (*stateFactory) ValidateProtocolContextOverride(profilevalue.ProtocolOverride) error {
	return nil
}
func (factory *stateFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	run := newStateRun()
	factory.mu.Lock()
	factory.runs = append(factory.runs, run)
	factory.mu.Unlock()
	factory.created <- run
	return run, nil
}
func (factory *stateFactory) count() int {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return len(factory.runs)
}
func (factory *stateFactory) wait(t *testing.T) *stateRun {
	t.Helper()
	select {
	case run := <-factory.created:
		return run
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for state Run")
		return nil
	}
}

type supervisorRetryPolicy struct {
	delay time.Duration
	ok    bool
}

func (policy supervisorRetryPolicy) Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool) {
	return policy.delay, policy.ok
}

type supervisorRetryScheduler struct {
	mu        sync.Mutex
	callbacks []func()
}

func (scheduler *supervisorRetryScheduler) Schedule(_ time.Duration, callback func()) session.RetryCancellation {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.callbacks = append(scheduler.callbacks, callback)
	return noOpRetryCancellation{}
}
func (scheduler *supervisorRetryScheduler) count() int {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return len(scheduler.callbacks)
}

func newHeldCancellationRun() *heldCancellationRun {
	return &heldCancellationRun{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
	}
}

func releaseHeldCancellationRun(run *heldCancellationRun) {
	select {
	case <-run.release:
	default:
		close(run.release)
	}
}

func (run *heldCancellationRun) Execute(ctx context.Context, _ protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	close(run.started)
	<-ctx.Done()
	close(run.canceled)
	<-run.release
	return nil
}

type unavailableRetryPolicy struct{}

func (unavailableRetryPolicy) Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool) {
	return 0, false
}

type noOpRetryScheduler struct{}

func (noOpRetryScheduler) Schedule(time.Duration, func()) session.RetryCancellation {
	return noOpRetryCancellation{}
}

type noOpRetryCancellation struct{}

func (noOpRetryCancellation) Cancel() {}

func testSupervisorDeps() Dependencies {
	return Dependencies{
		Now:            func() time.Time { return time.Unix(100, 0) },
		RetryPolicy:    unavailableRetryPolicy{},
		RetryScheduler: noOpRetryScheduler{},
	}
}

func testRuntimeDefinition() session.RuntimeDefinition {
	return session.RuntimeDefinition{
		Configuration: session.Configuration{
			DisplayName:          "Campus network",
			InstitutionProfileID: "profile-1",
			NetworkBindingPolicy: session.NetworkBindingPolicy{
				Mode: session.AutomaticallySelectLatestAvailable,
			},
			ProtocolContextOverride: profilevalue.ProtocolOverride([]byte(`{"network":"campus"}`)),
		},
		AuthenticationCredential: credential.AuthenticationCredential{
			Username: "test-account",
			Password: "secret-password",
		},
		InstitutionProfile: profilevalue.Profile{
			InstitutionProfileID:             "profile-1",
			DisplayName:                      "Campus",
			AuthenticationProtocolID:         "test-protocol",
			InstitutionProtocolConfiguration: profilevalue.ProtocolConfiguration([]byte(`{"realm":"campus"}`)),
		},
		AuthenticationProtocolFactory: stubFactory{},
		AutoReconnect:                 true,
		SystemHostInformation: environment.SystemHostInformation{
			HostName:              "test-host",
			OperatingSystemFamily: "test-os",
			MachineArchitecture:   "test-architecture",
		},
	}
}

func waitForSupervisorState(t *testing.T, supervisor *Supervisor, id ID, want session.State) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		snapshot, err := supervisor.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get error while waiting for state %q: %v", want, err)
		}
		if snapshot.State == want {
			return snapshot
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for state %q; latest state %q", want, snapshot.State)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func supervisorUsableNetworkSnapshot(t *testing.T, revision uint64) environment.Snapshot {
	t.Helper()
	networkInterface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:              "ethernet-1",
		DisplayName:              "Ethernet",
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
		t.Fatalf("NewNetworkInterface error: %v", err)
	}
	return environment.NewSnapshot(revision, time.Unix(int64(revision), 0), []environment.NetworkInterface{networkInterface})
}

func (stubFactory) ValidateUsername(string) error             { return nil }
func (*sequentialHeldFactory) ValidateUsername(string) error  { return nil }
func (heldCancellationFactory) ValidateUsername(string) error { return nil }
func (*stateFactory) ValidateUsername(string) error           { return nil }

// closeTestSupervisor joins all remaining actors when a test doesn't assert
// the Close result itself.
func closeTestSupervisor(supervisor *Supervisor) {
	_ = supervisor.Close()
	supervisor.Wait()
}
