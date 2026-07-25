package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

type controlledFactory struct {
	mu               sync.Mutex
	inputs           []protocol.AuthenticationProtocolRunCreationInputs
	runs             []*controlledRun
	holdCancellation bool
	creationError    error
}

func (factory *controlledFactory) ProtocolID() protocol.AuthenticationProtocolID {
	return "test-protocol"
}

func (factory *controlledFactory) ValidateInstitutionProtocolConfiguration(protocol.InstitutionProtocolConfiguration) error {
	return nil
}

func (factory *controlledFactory) ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride) error {
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
