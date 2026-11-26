package operations

import (
	"context"
	"path/filepath"

	"sidravia/internal/update"
)

// AcquireStart holds process admission against file application. Linux callers
// retain this shared lease until exit; only an updater handoff may release early.
// A persisted blocked phase survives loss of the executor's kernel lock.
func (s *Store) AcquireStart(ctx context.Context) (func(), error) {
	return s.admit(ctx, "")
}

// AcquireExecution admits exactly the current, quiescent operation. Hold the
// returned release until disk application and its durable terminal write finish.
// This does not replace process quiescence or signed on-disk byte verification.
func (s *Store) AcquireExecution(ctx context.Context, id string) (func(), error) {
	if !update.ValidOperationID(id) {
		return nil, ErrInvalid
	}
	return s.admit(ctx, id)
}
func (s *Store) admit(ctx context.Context, id string) (func(), error) {
	// Prepare the secured directory and reject aliases before opening the lock.
	if _, _, err := s.Read(ctx); err != nil {
		return nil, err
	}
	release, err := admissionLock(filepath.Join(s.directory, "execution.lock"), id != "")
	if err != nil {
		return nil, err
	}
	r, exists, err := s.Read(ctx)
	if err == nil {
		if id == "" {
			if targetSealed(s.target) || exists && r.RequiresStartupBlock() {
				err = ErrConflict
			}
		} else if !exists || r.ID != id || (r.Phase != Quiescent && r.Phase != RollingBack) {
			err = ErrConflict
		}
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// AcquireCoordinator admits a single lifecycle owner without excluding already
// running application leases needed to negotiate a normal exit.
func (s *Store) AcquireCoordinator(ctx context.Context, id string) (func(), error) {
	return s.acquireCoordinator(ctx, id, false)
}

func (s *Store) AcquireRollbackCoordinator(ctx context.Context, id string) (func(), error) {
	return s.acquireCoordinator(ctx, id, true)
}

func (s *Store) acquireCoordinator(ctx context.Context, id string, rollback bool) (func(), error) {
	if !update.ValidOperationID(id) {
		return nil, ErrInvalid
	}
	if _, _, err := s.Read(ctx); err != nil {
		return nil, err
	}
	release, err := lock(filepath.Join(s.directory, "coordinator.lock"))
	if err != nil {
		return nil, err
	}
	r, exists, err := s.Read(ctx)
	if err == nil {
		switch {
		case !exists || r.ID != id:
			err = ErrConflict
		case rollback:
			switch r.Phase {
			case Preparing, Quiescent, SourceStarting, Applying, RepairRequired, Applied, ServiceStarting, RollingBack, RolledBack, Aborted, Completed:
			default:
				err = ErrConflict
			}
			if r.Recovery == nil || !r.Recovery.ManagedService {
				err = ErrConflict
			}
		case r.Phase != Verified:
			err = ErrConflict
		}
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}
