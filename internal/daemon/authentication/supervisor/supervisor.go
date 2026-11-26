package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	environment "sidravia/internal/connection/environment"
	"sidravia/internal/daemon/authentication/session"
)

// ID aliases session.AuthenticationSessionID for ergonomic supervisor API.
type ID = session.AuthenticationSessionID

// Snapshot aliases session.Snapshot.
type Snapshot = session.Snapshot

// Dependencies provides session-level dependencies that the Supervisor
// passes to each created session.
type Dependencies struct {
	Now                        func() time.Time
	RetryPolicy                session.RetryPolicy
	RetryScheduler             session.RetryScheduler
	Diagnostics                session.Diagnostics
	ProtocolDiagnosticsFactory session.ProtocolDiagnosticsFactory
}

// sessionState tracks the lifecycle state of a managed session.
type sessionState int

const (
	stateActive   sessionState = iota // session is running
	stateStopping                     // Stop called, Suspend in progress, counts as active for admission
	stateStopped                      // Suspend completed, available for Restart or ForgetStopped
	stateStarting                     // Restart called, Restart in progress, counts as active for admission
)

// Supervisor manages authentication sessions with single-active admission.
// At most one non-stopped MaintainAuthentication session may exist at a time.
type Supervisor struct {
	mu                  sync.Mutex // protects the session registry, admission state, subscriptions and latest network
	sessions            map[ID]*managedSession
	sessionSequence     uint64
	sessionDependencies session.Dependencies
	subscriptions       map[*Subscription]struct{}
	closed              atomic.Bool
	closeOnce           sync.Once
	revisionForwarders  sync.WaitGroup
	latestNetwork       environment.Snapshot
	hasLatestNetwork    bool
}

type managedSession struct {
	actor                 *session.AuthenticationSession
	intent                session.Intent
	state                 sessionState
	operationMu           sync.Mutex
	reserved              bool
	retiring              bool // shutdown started; network delivery no longer needs this actor
	stateChange           chan struct{}
	stopForwarding        chan struct{}
	stopForwardingOnce    sync.Once
	forwardDone           chan struct{} // closed when forward goroutine exits
	lastForwardedRevision uint64        // paired delivery metadata, protected by Supervisor.mu
}

// New creates a Supervisor with the given session-level dependencies.
func New(deps Dependencies) *Supervisor {
	return &Supervisor{
		sessions: make(map[ID]*managedSession),
		sessionDependencies: session.Dependencies{
			Now:                        deps.Now,
			RetryPolicy:                deps.RetryPolicy,
			RetryScheduler:             deps.RetryScheduler,
			Diagnostics:                deps.Diagnostics,
			ProtocolDiagnosticsFactory: deps.ProtocolDiagnosticsFactory,
		},
	}
}

// StartResolved allocates a SessionID, checks single-active admission,
// creates and starts a session, and begins forwarding its revision events.
//
// The caller's context is respected for the initial snapshot. If the
// snapshot fails, successful shutdown drains and removes the session. A
// shutdown failure retains the resource and joins its cause with the start error.
func (s *Supervisor) StartResolved(
	ctx context.Context,
	definition session.RuntimeDefinition,
	intent session.Intent,
) (ID, Snapshot, error) {
	s.mu.Lock()

	if s.closed.Load() {
		s.mu.Unlock()
		return "", Snapshot{}, fmt.Errorf("supervisor is closed")
	}

	if intent == session.MaintainAuthentication {
		for _, ms := range s.sessions {
			if (ms.state != stateStopped || ms.reserved) && ms.intent == session.MaintainAuthentication {
				s.mu.Unlock()
				return "", Snapshot{}, fmt.Errorf("%w", ErrActiveSessionConflict)
			}
		}
	}

	s.sessionSequence++
	id := ID(fmt.Sprintf("session-%d", s.sessionSequence))
	definition.Configuration.AuthenticationSessionID = id

	if err := definition.Validate(); err != nil {
		s.mu.Unlock()
		return "", Snapshot{}, fmt.Errorf("invalid runtime definition: %w", err)
	}

	actor, err := session.NewAuthenticationSession(definition, intent, s.sessionDependencies)
	if err != nil {
		s.mu.Unlock()
		return "", Snapshot{}, fmt.Errorf("create session: %w", err)
	}
	actor.Start()

	ms := &managedSession{
		actor:          actor,
		intent:         intent,
		state:          stateActive,
		stateChange:    make(chan struct{}),
		stopForwarding: make(chan struct{}),
		forwardDone:    make(chan struct{}),
	}
	s.sessions[id] = ms

	s.revisionForwarders.Add(1)
	go func() {
		defer close(ms.forwardDone)
		s.forwardRevisions(id, ms)
	}()

	// Copy the current latest network snapshot under the lock so the new
	// session cannot miss the newest revision already accepted by the
	// Supervisor. environment.Snapshot is an immutable value boundary, so a
	// plain value copy is sufficient.
	latestNetwork := s.latestNetwork
	hasLatestNetwork := s.hasLatestNetwork
	s.mu.Unlock()

	// When a latest snapshot exists, deliver it to the new Session before
	// querying its initial public Snapshot, so the returned Snapshot already
	// reflects the selected network binding and may be authenticating.
	if hasLatestNetwork {
		if _, err := actor.ApplySystemNetworkSnapshot(ctx, latestNetwork); err != nil {
			return "", Snapshot{}, s.rollbackNewSession(id, ms, fmt.Errorf("apply latest network snapshot: %w", err))
		}
	}

	// The caller's context is respected for the initial snapshot.
	snapshot, err := actor.Snapshot(ctx)
	if err != nil {
		return "", Snapshot{}, s.rollbackNewSession(id, ms, fmt.Errorf("initial snapshot: %w", err))
	}
	if snapshot.State == session.Suspended {
		s.mu.Lock()
		if current, ok := s.sessions[id]; ok && current == ms {
			ms.state = stateStopped
		}
		s.mu.Unlock()
	}

	return id, snapshot, nil
}

// rollbackNewSession preserves the start cause and retains the resource when
// shutdown fails. A successful shutdown drains publications before removal.
func (s *Supervisor) rollbackNewSession(id ID, ms *managedSession, cause error) error {
	if shutdownErr := s.shutdownAndRemove(id, ms); shutdownErr != nil {
		return errors.Join(cause, fmt.Errorf("shutdown after failed start: %w", shutdownErr))
	}
	return cause
}

func (s *Supervisor) shutdownAndRemove(id ID, ms *managedSession) error {
	s.mu.Lock()
	ms.retiring = true
	s.mu.Unlock()
	if err := ms.actor.Shutdown(context.Background()); err != nil {
		return err
	}
	s.finishRemoval(id, ms)
	return nil
}

// finishRemoval waits without the map lock. Close may stop the forwarder before
// the producer has drained; in that case it owns stream-wide discontinuity.
func (s *Supervisor) finishRemoval(id ID, ms *managedSession) {
	<-ms.forwardDone
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return
	}
	if current, ok := s.sessions[id]; ok && current == ms {
		s.publishStateEventLocked(StateEvent{Kind: StateSessionRemoved, SessionID: id, Revision: ms.lastForwardedRevision})
		delete(s.sessions, id)
	}
}

// Stop suspends a session, keeping its SessionID and Snapshot.
func (s *Supervisor) Stop(ctx context.Context, id ID) (Snapshot, error) {
	ms, err := s.managed(id)
	if err != nil {
		return Snapshot{}, err
	}
	ms.operationMu.Lock()
	defer ms.operationMu.Unlock()
	return s.stopManaged(ctx, id, ms)
}

func (s *Supervisor) stopManaged(ctx context.Context, id ID, ms *managedSession) (Snapshot, error) {
	s.mu.Lock()
	current, ok := s.sessions[id]
	if !ok || current != ms {
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	switch ms.state {
	case stateStopping, stateStopped:
		s.mu.Unlock()
		snapshot, err := ms.actor.Snapshot(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("snapshot session %q during repeated stop: %w", id, err)
		}
		s.observeStoppedRevision(id, ms, snapshot)
		return snapshot, nil
	case stateActive:
	default:
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("%w: %q", ErrSessionStateConflict, id)
	}
	ms.state = stateStopping
	s.mu.Unlock()

	snapshot, err := ms.actor.Suspend(ctx)
	if err != nil {
		s.reconcileStopError(id, ms)
		return Snapshot{}, fmt.Errorf("suspend session %q: %w", id, err)
	}
	// An already-suspended Session treats Stop as a no-op and emits no new
	// revision, so align the private marker from the authoritative reply.
	s.observeStoppedRevision(id, ms, snapshot)

	return snapshot, nil
}

func (s *Supervisor) managed(id ID) (*managedSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ms, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	return ms, nil
}

func (s *Supervisor) reconcileStopError(id ID, managed *managedSession) {
	snapshot, err := managed.actor.Snapshot(context.Background())
	if err == nil {
		switch snapshot.State {
		case session.Stopping:
			return
		case session.Suspended:
			s.observeStoppedRevision(id, managed, snapshot)
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.sessions[id]
	if exists && current == managed && managed.state == stateStopping {
		managed.state = stateActive
	}
}

// EnsureRunning non-disruptively ensures that a retained session is active.
func (s *Supervisor) EnsureRunning(ctx context.Context, id ID) (Snapshot, error) {
	ms, err := s.managed(id)
	if err != nil {
		return Snapshot{}, err
	}
	ms.operationMu.Lock()
	defer ms.operationMu.Unlock()

	snapshot, err := ms.actor.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot session %q: %w", id, err)
	}
	if snapshot.State != session.Suspended && snapshot.State != session.Stopping {
		return snapshot, nil
	}
	if err := s.reserveAdmission(id, ms); err != nil {
		return Snapshot{}, err
	}
	defer s.releaseReservation(id, ms)
	if snapshot.State == session.Stopping {
		if err := s.waitStopped(ctx, id, ms); err != nil {
			return Snapshot{}, err
		}
	}
	snapshot, err = ms.actor.Activate(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("activate session %q: %w", id, err)
	}
	s.markActive(id, ms)
	return snapshot, nil
}

// Restart deliberately restarts a retained session from every public state.
func (s *Supervisor) Restart(ctx context.Context, id ID) (Snapshot, error) {
	ms, err := s.managed(id)
	if err != nil {
		return Snapshot{}, err
	}
	ms.operationMu.Lock()
	defer ms.operationMu.Unlock()

	snapshot, err := ms.actor.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot session %q: %w", id, err)
	}
	if snapshot.State == session.Stopping || snapshot.State == session.Suspended {
		if err := s.reserveAdmission(id, ms); err != nil {
			return Snapshot{}, err
		}
		defer s.releaseReservation(id, ms)
		if snapshot.State == session.Stopping {
			if err := s.waitStopped(ctx, id, ms); err != nil {
				return Snapshot{}, err
			}
		}
	}
	snapshot, err = ms.actor.Restart(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("restart session %q: %w", id, err)
	}
	s.markActive(id, ms)
	return snapshot, nil
}

// Remove stops, shuts down and forgets a retained session atomically.
func (s *Supervisor) Remove(ctx context.Context, id ID) error {
	ms, err := s.managed(id)
	if err != nil {
		return err
	}
	ms.operationMu.Lock()
	defer ms.operationMu.Unlock()
	if err := s.reserveDeletion(id, ms); err != nil {
		return err
	}
	defer s.releaseReservation(id, ms)

	snapshot, err := ms.actor.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot session %q: %w", id, err)
	}
	if snapshot.State != session.Suspended {
		if snapshot.State != session.Stopping {
			if _, err := s.stopManaged(ctx, id, ms); err != nil {
				return err
			}
		}
		if err := s.waitStopped(ctx, id, ms); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.shutdownAndRemove(id, ms); err != nil {
		return fmt.Errorf("shutdown session %q: %w", id, err)
	}
	return nil
}

func (s *Supervisor) waitStopped(ctx context.Context, id ID, ms *managedSession) error {
	for {
		s.mu.Lock()
		current, ok := s.sessions[id]
		if !ok || current != ms {
			s.mu.Unlock()
			return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
		}
		if ms.state == stateStopped {
			s.mu.Unlock()
			return nil
		}
		changed := ms.stateChange
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ForgetStopped removes a stopped session from memory and shuts it down.
// Returns an error if the session is still active.
func (s *Supervisor) ForgetStopped(id ID) error {
	ms, err := s.managed(id)
	if err != nil {
		return err
	}
	ms.operationMu.Lock()
	defer ms.operationMu.Unlock()
	s.mu.Lock()
	current, ok := s.sessions[id]
	if !ok || current != ms {
		s.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if ms.state != stateStopped {
		s.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrSessionStateConflict, id)
	}
	ms.reserved = true
	s.mu.Unlock()
	defer s.releaseReservation(id, ms)
	return s.shutdownAndRemove(id, ms)
}

// Get returns the latest snapshot for the given session.
func (s *Supervisor) Get(ctx context.Context, id ID) (Snapshot, error) {
	s.mu.Lock()
	ms, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	snapshot, err := ms.actor.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s.observeStoppedRevision(id, ms, snapshot)
	return snapshot, nil
}

// GetDiagnosticSnapshot reads one managed actor without starting a Run or
// holding the Supervisor map mutex while waiting for its reply.
func (s *Supervisor) GetDiagnosticSnapshot(ctx context.Context, id ID) (session.DiagnosticSnapshot, error) {
	ms, err := s.managed(id)
	if err != nil {
		return session.DiagnosticSnapshot{}, err
	}
	return ms.actor.QueryDiagnosticSnapshot(ctx)
}

// GetBusiness reads the Session-owned business snapshot without network activity.
func (s *Supervisor) GetBusiness(ctx context.Context, id ID) (session.BusinessSnapshot, error) {
	ms, err := s.managed(id)
	if err != nil {
		return session.BusinessSnapshot{}, err
	}
	return ms.actor.Business(ctx)
}

// List returns snapshots for all known sessions.
func (s *Supervisor) List(ctx context.Context) ([]Snapshot, error) {
	s.mu.Lock()
	actors := make([]*session.AuthenticationSession, 0, len(s.sessions))
	for _, ms := range s.sessions {
		actors = append(actors, ms.actor)
	}
	s.mu.Unlock()

	snapshots := make([]Snapshot, 0, len(actors))
	for _, actor := range actors {
		snapshot, err := actor.Snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("snapshot session: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(left, right int) bool {
		return snapshots[left].AuthenticationSessionID < snapshots[right].AuthenticationSessionID
	})
	return snapshots, nil
}

// Close ends all subscriptions with connection-wide discontinuity before
// shutting down actors and waiting for owned forwarders. It is idempotent.
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed.Store(true)
		sessions := make([]*managedSession, 0, len(s.sessions))
		for _, ms := range s.sessions {
			sessions = append(sessions, ms)
			ms.stopForwardingOnce.Do(func() { close(ms.stopForwarding) })
		}
		for sub := range s.subscriptions {
			sub.closeWithCause(ErrSubscriptionShutdown)
		}
		clear(s.subscriptions)
		s.mu.Unlock()
		for _, ms := range sessions {
			// Preserve the existing whole-Supervisor Close return contract.
			_ = ms.actor.Shutdown(context.Background())
		}
		s.revisionForwarders.Wait()
	})
	return nil
}

// Wait blocks until all internal goroutines have exited.
func (s *Supervisor) Wait() {
	s.revisionForwarders.Wait()
}
