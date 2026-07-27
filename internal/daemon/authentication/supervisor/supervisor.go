package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"sidravia/internal/daemon/authentication/session"
	environment "sidravia/internal/daemon/environment"
)

// ID aliases session.AuthenticationSessionID for ergonomic supervisor API.
type ID = session.AuthenticationSessionID

// Snapshot aliases session.Snapshot.
type Snapshot = session.Snapshot

// RevisionEvent carries a session revision and its latest complete snapshot.
type RevisionEvent struct {
	SessionID ID
	Revision  uint64
	Snapshot  Snapshot
}

// Dependencies provides session-level dependencies that the Supervisor
// passes to each created session.
type Dependencies struct {
	Now            func() time.Time
	RetryPolicy    session.RetryPolicy
	RetryScheduler session.RetryScheduler
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
	mu               sync.Mutex
	sessions         map[ID]*managedSession
	counter          uint64
	deps             session.Dependencies
	subs             []chan RevisionEvent
	closed           atomic.Bool
	closeOnce        sync.Once
	wg               sync.WaitGroup
	latestNetwork    environment.Snapshot
	hasLatestNetwork bool
}

type managedSession struct {
	actor       *session.AuthenticationSession
	intent      session.Intent
	state       sessionState
	stopFwd     chan struct{}
	stopFwdOnce sync.Once
	forwardDone chan struct{} // closed when forward goroutine exits
}

// New creates a Supervisor with the given session-level dependencies.
func New(deps Dependencies) *Supervisor {
	return &Supervisor{
		sessions: make(map[ID]*managedSession),
		deps: session.Dependencies{
			Now:            deps.Now,
			RetryPolicy:    deps.RetryPolicy,
			RetryScheduler: deps.RetryScheduler,
		},
	}
}

// StartResolved allocates a SessionID, checks single-active admission,
// creates and starts a session, and begins forwarding its revision events.
//
// The caller's context is respected for the initial snapshot. If the
// snapshot fails, the session is fully rolled back: removed from the
// supervisor, forward goroutine stopped, and session shut down.
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
			if ms.state != stateStopped && ms.intent == session.MaintainAuthentication {
				s.mu.Unlock()
				return "", Snapshot{}, fmt.Errorf("another maintain_authentication session is active")
			}
		}
	}

	s.counter++
	id := ID(fmt.Sprintf("session-%d", s.counter))
	definition.Configuration.AuthenticationSessionID = id

	if err := definition.Validate(); err != nil {
		s.mu.Unlock()
		return "", Snapshot{}, fmt.Errorf("invalid runtime definition: %w", err)
	}

	actor, err := session.NewAuthenticationSession(definition, intent, s.deps)
	if err != nil {
		s.mu.Unlock()
		return "", Snapshot{}, fmt.Errorf("create session: %w", err)
	}
	actor.Start()

	ms := &managedSession{
		actor:       actor,
		intent:      intent,
		state:       stateActive,
		stopFwd:     make(chan struct{}),
		forwardDone: make(chan struct{}),
	}
	s.sessions[id] = ms

	s.wg.Add(1)
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

	return id, snapshot, nil
}

// rollbackNewSession fully rolls back a newly created Session that failed
// during start: it removes the Session from the map, stops its revision
// forwarder, waits for the forward goroutine to exit, and shuts down the
// actor. The supplied cause is returned; if shutdown itself fails, the cause is
// joined with the shutdown error. Both the latest-snapshot application failure
// and the initial Snapshot failure share this identical rollback path.
func (s *Supervisor) rollbackNewSession(id ID, ms *managedSession, cause error) error {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	ms.stopFwdOnce.Do(func() { close(ms.stopFwd) })
	<-ms.forwardDone
	if shutdownErr := ms.actor.Shutdown(context.Background()); shutdownErr != nil {
		return errors.Join(cause, fmt.Errorf("shutdown after failed start: %w", shutdownErr))
	}
	return cause
}

// Stop suspends a session, keeping its SessionID and Snapshot.
func (s *Supervisor) Stop(ctx context.Context, id ID) (Snapshot, error) {
	s.mu.Lock()
	ms, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("session %q not found", id)
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
		return Snapshot{}, fmt.Errorf("session %q is not active", id)
	}
	ms.state = stateStopping
	s.mu.Unlock()

	snapshot, err := ms.actor.Suspend(ctx)
	if err != nil {
		s.mu.Lock()
		if ms.state == stateStopping {
			ms.state = stateActive
		}
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("suspend session %q: %w", id, err)
	}
	// An already-suspended Session treats Stop as a no-op and emits no new
	// revision, so align the private marker from the authoritative reply.
	s.observeStoppedRevision(id, ms, snapshot)

	return snapshot, nil
}

// Restart resumes a suspended session if single-active admission allows.
func (s *Supervisor) Restart(ctx context.Context, id ID) (Snapshot, error) {
	s.mu.Lock()
	ms, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("session %q not found", id)
	}

	if ms.state != stateStopped {
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("session %q is not stopped", id)
	}

	if ms.intent == session.MaintainAuthentication {
		for otherID, other := range s.sessions {
			if otherID == id {
				continue
			}
			if other.state != stateStopped && other.intent == session.MaintainAuthentication {
				s.mu.Unlock()
				return Snapshot{}, fmt.Errorf("another maintain_authentication session is active")
			}
		}
	}
	ms.state = stateStarting
	s.mu.Unlock()

	snapshot, err := ms.actor.Restart(ctx)
	if err != nil {
		s.mu.Lock()
		ms.state = stateStopped
		s.mu.Unlock()
		return Snapshot{}, fmt.Errorf("restart session %q: %w", id, err)
	}

	s.mu.Lock()
	ms.state = stateActive
	s.mu.Unlock()

	return snapshot, nil
}

// ForgetStopped removes a stopped session from memory and shuts it down.
// Returns an error if the session is still active.
func (s *Supervisor) ForgetStopped(id ID) error {
	s.mu.Lock()
	ms, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	if ms.state != stateStopped {
		s.mu.Unlock()
		return fmt.Errorf("session %q is still active", id)
	}
	delete(s.sessions, id)
	ms.stopFwdOnce.Do(func() { close(ms.stopFwd) })
	s.mu.Unlock()

	return ms.actor.Shutdown(context.Background())
}

// Get returns the latest snapshot for the given session.
func (s *Supervisor) Get(ctx context.Context, id ID) (Snapshot, error) {
	s.mu.Lock()
	ms, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return Snapshot{}, fmt.Errorf("session %q not found", id)
	}
	snapshot, err := ms.actor.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s.observeStoppedRevision(id, ms, snapshot)
	return snapshot, nil
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
	return snapshots, nil
}

// RevisionEvents returns a new channel that receives revision events from
// all sessions. If the Supervisor is closed, returns a closed channel.
func (s *Supervisor) RevisionEvents() <-chan RevisionEvent {
	ch := make(chan RevisionEvent, 16)
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		close(ch)
		return ch
	}
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// Close shuts down all sessions, stops forwarding, waits for goroutines,
// then closes all subscriber channels. It is idempotent.
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed.Store(true)
		sessions := make([]*managedSession, 0, len(s.sessions))
		for _, ms := range s.sessions {
			sessions = append(sessions, ms)
		}
		subs := s.subs
		s.subs = nil
		s.mu.Unlock()

		for _, ms := range sessions {
			_ = ms.actor.Shutdown(context.Background())
			ms.stopFwdOnce.Do(func() { close(ms.stopFwd) })
		}

		// Wait for all forward goroutines to exit before closing subscriber channels.
		s.wg.Wait()

		for _, sub := range subs {
			close(sub)
		}
	})
	return nil
}

// Wait blocks until all internal goroutines have exited.
func (s *Supervisor) Wait() {
	s.wg.Wait()
}

func (s *Supervisor) forwardRevisions(id ID, managed *managedSession) {
	defer s.wg.Done()
	revisions := managed.actor.RevisionEvents()
	for {
		select {
		case event, ok := <-revisions:
			if !ok {
				return
			}
			snapshot, err := managed.actor.Snapshot(context.Background())
			if err != nil {
				continue
			}
			s.observeStoppedRevision(id, managed, snapshot)
			s.publishRevision(RevisionEvent{
				SessionID: id,
				Revision:  event.Revision,
				Snapshot:  snapshot,
			})
		case <-managed.stopFwd:
			return
		}
	}
}

func (s *Supervisor) observeStoppedRevision(id ID, managed *managedSession, snapshot Snapshot) {
	if snapshot.State != session.Suspended {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.sessions[id]
	if exists && current == managed && managed.state == stateStopping {
		managed.state = stateStopped
	}
}

func (s *Supervisor) publishRevision(event RevisionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		select {
		case sub <- event:
		default:
			select {
			case <-sub:
			default:
			}
			select {
			case sub <- event:
			default:
			}
		}
	}
}
