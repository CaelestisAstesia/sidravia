package supervisor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"sidravia/internal/daemon/authentication/session"
	environment "sidravia/internal/daemon/environment"
)

// networkSnapshotTarget pairs a SessionID with its actor for deterministic
// snapshot delivery.
type networkSnapshotTarget struct {
	id    ID
	actor *session.AuthenticationSession
}

// ApplySystemNetworkSnapshot accepts a typed system network snapshot, makes it
// the Supervisor's authoritative latest snapshot when its revision is newer
// than the stored one, and delivers the authoritative snapshot to every
// currently known Session in deterministic ascending SessionID order.
//
// Revision rules (the only Supervisor-level network rule):
//   - A newer revision becomes the authoritative latest snapshot and is
//     delivered to all known Sessions.
//   - An older revision is ignored and returns nil.
//   - An equal revision re-delivers the already stored authoritative snapshot,
//     not the caller's possibly different same-revision value.
//
// The method creates no goroutine and never holds the Supervisor mutex while
// calling a Session. A nil or already-cancelled context, or a closed
// Supervisor, returns an error without changing the stored snapshot or
// delivering anything. A delivery failure does not roll back the latest
// snapshot or Sessions that already accepted it; repeating the same revision
// is the recovery operation.
func (s *Supervisor) ApplySystemNetworkSnapshot(
	ctx context.Context,
	snapshot environment.Snapshot,
) error {
	if ctx == nil {
		return errors.New("supervisor: context is required for applying a network snapshot")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("supervisor: apply network snapshot: %w", err)
	}

	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return errors.New("supervisor is closed")
	}

	var toDeliver environment.Snapshot
	switch {
	case !s.hasLatestNetwork:
		s.latestNetwork = snapshot
		s.hasLatestNetwork = true
		toDeliver = snapshot
	case snapshot.Revision > s.latestNetwork.Revision:
		s.latestNetwork = snapshot
		toDeliver = snapshot
	case snapshot.Revision == s.latestNetwork.Revision:
		// Replay the stored authoritative snapshot, not the caller's value.
		toDeliver = s.latestNetwork
	default:
		// Older revision: ignore without changing state or delivering.
		s.mu.Unlock()
		return nil
	}

	targets := make([]networkSnapshotTarget, 0, len(s.sessions))
	for id, ms := range s.sessions {
		targets = append(targets, networkSnapshotTarget{id: id, actor: ms.actor})
	}
	s.mu.Unlock()

	slices.SortFunc(targets, func(a, b networkSnapshotTarget) int {
		return strings.Compare(string(a.id), string(b.id))
	})

	return s.deliverNetworkSnapshot(ctx, toDeliver, targets)
}

// deliverNetworkSnapshot delivers the authoritative snapshot to each target
// Session in the given order. It continues after a failure and joins all
// errors, preserving the affected SessionID and original cause. It does not
// hold the Supervisor mutex and creates no goroutine.
func (s *Supervisor) deliverNetworkSnapshot(
	ctx context.Context,
	snapshot environment.Snapshot,
	targets []networkSnapshotTarget,
) error {
	var errs []error
	for _, target := range targets {
		if _, err := target.actor.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
			errs = append(errs, fmt.Errorf("deliver network snapshot to session %q: %w", target.id, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
