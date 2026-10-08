package supervisor

import (
	"context"
	"errors"
	"sync"

	environment "sidravia/internal/daemon/environment"
)

type StateEventKind string

const (
	StateSessionChanged StateEventKind = "session.changed"
	StateSessionRemoved StateEventKind = "session.removed"
	StateNetworkChanged StateEventKind = "network.changed"
)

// StateEvent owns the complete facts paired with one resource revision.
type StateEvent struct {
	Kind            StateEventKind
	SessionID       ID
	Revision        uint64
	Snapshot        Snapshot
	NetworkSnapshot environment.Snapshot
}

// StateStreamError is a stable termination cause. Shutdown, overflow and an
// invalid publication mean the consumer must obtain fresh authoritative facts.
type StateStreamError string

func (err StateStreamError) Error() string { return string(err) }

const (
	ErrSubscriptionClosed       StateStreamError = "supervisor subscription closed"
	ErrSubscriptionShutdown     StateStreamError = "supervisor subscription shutdown: state stream discontinuity"
	ErrSubscriptionOverflow     StateStreamError = "supervisor subscription overflow: state stream discontinuity"
	ErrSubscriptionInvalidEvent StateStreamError = "supervisor invalid state event: state stream discontinuity"
)

const subscriptionCapacity = 256

type resourceKey struct {
	sessionID ID
	network   bool
}

// Subscription has no worker goroutine. The owner must Close it when finished.
// Supervisor.mu precedes mu; Next never calls its owner while holding mu.
type Subscription struct {
	owner   *Supervisor
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelCauseFunc
	pending map[resourceKey]StateEvent
	keys    []resourceKey
	wake    chan struct{}
}

func (s *Supervisor) Subscribe() (*Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return nil, ErrSubscriptionShutdown
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	sub := &Subscription{owner: s, ctx: ctx, cancel: cancel, pending: make(map[resourceKey]StateEvent), keys: make([]resourceKey, 0, subscriptionCapacity), wake: make(chan struct{}, 1)}
	if s.subs == nil {
		s.subs = make(map[*Subscription]struct{})
	}
	s.subs[sub] = struct{}{}
	return sub, nil
}

func (sub *Subscription) Next(ctx context.Context) (StateEvent, error) {
	if ctx == nil {
		return StateEvent{}, errors.New("supervisor subscription: context is required")
	}
	for {
		if err := context.Cause(ctx); err != nil {
			return StateEvent{}, err
		}
		sub.mu.Lock()
		if err := context.Cause(sub.ctx); err != nil {
			sub.mu.Unlock()
			return StateEvent{}, err
		}
		if len(sub.keys) > 0 {
			key := sub.keys[0]
			event := sub.pending[key]
			delete(sub.pending, key)
			copy(sub.keys, sub.keys[1:])
			sub.keys = sub.keys[:len(sub.keys)-1]
			if len(sub.keys) > 0 {
				sub.signalLocked()
			}
			sub.mu.Unlock()
			return event.clone(), nil
		}
		sub.mu.Unlock()
		select {
		case <-ctx.Done():
			return StateEvent{}, context.Cause(ctx)
		case <-sub.ctx.Done():
			return StateEvent{}, context.Cause(sub.ctx)
		case <-sub.wake:
		}
	}
}

func (sub *Subscription) Close() {
	sub.owner.mu.Lock()
	delete(sub.owner.subs, sub)
	sub.closeWithCause(ErrSubscriptionClosed)
	sub.owner.mu.Unlock()
}

func (sub *Subscription) closeWithCause(cause error) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if context.Cause(sub.ctx) != nil {
		return
	}
	clear(sub.pending)
	sub.keys = sub.keys[:0]
	sub.cancel(cause)
}

func (sub *Subscription) signalLocked() {
	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

// enqueue is called only with the Supervisor lock. false unregisters this
// subscription after overflow; other consumers retain their own queues.
func (sub *Subscription) enqueue(event StateEvent) bool {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if context.Cause(sub.ctx) != nil {
		return false
	}
	key := resourceKey{sessionID: event.SessionID, network: event.Kind == StateNetworkChanged}
	previous, exists := sub.pending[key]
	if exists {
		// Terminal has precedence even when it shares the final actor revision.
		if previous.Kind == StateSessionRemoved || event.Revision < previous.Revision {
			return true
		}
	} else {
		if len(sub.keys) == subscriptionCapacity {
			clear(sub.pending)
			sub.keys = sub.keys[:0]
			sub.cancel(ErrSubscriptionOverflow)
			return false
		}
		sub.keys = append(sub.keys, key)
	}
	sub.pending[key] = event.clone()
	sub.signalLocked()
	return true
}

func (event StateEvent) clone() StateEvent {
	event.Snapshot = event.Snapshot.Clone()
	event.NetworkSnapshot = cloneNetworkSnapshot(event.NetworkSnapshot)
	return event
}

func cloneNetworkSnapshot(snapshot environment.Snapshot) environment.Snapshot {
	return environment.NewSnapshot(snapshot.Revision, snapshot.ObservedAt, snapshot.Interfaces())
}

func (event StateEvent) valid() bool {
	emptyNetwork := event.NetworkSnapshot.Revision == 0 && event.NetworkSnapshot.ObservedAt.IsZero() && len(event.NetworkSnapshot.Interfaces()) == 0
	switch event.Kind {
	case StateSessionChanged:
		return event.SessionID != "" && event.Revision > 0 && event.SessionID == event.Snapshot.AuthenticationSessionID && event.Revision == event.Snapshot.Revision && emptyNetwork
	case StateSessionRemoved:
		return event.SessionID != "" && event.Revision > 0 && event.Snapshot == (Snapshot{}) && emptyNetwork
	case StateNetworkChanged:
		return event.SessionID == "" && event.Revision == 0 && event.Snapshot == (Snapshot{})
	default:
		return false
	}
}

func (s *Supervisor) publishStateEvent(event StateEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishStateEventLocked(event)
}

func (s *Supervisor) publishStateEventLocked(event StateEvent) {
	if s.closed.Load() {
		return
	}
	if !event.valid() {
		for sub := range s.subs {
			sub.closeWithCause(ErrSubscriptionInvalidEvent)
			delete(s.subs, sub)
		}
		return
	}
	for sub := range s.subs {
		if !sub.enqueue(event) {
			delete(s.subs, sub)
		}
	}
}
