package operations

import (
	"context"
	"os"
	"path/filepath"
	"sidravia/internal/update"
)

// DefaultRoot is shared by updater status and portable process admission. It
// stays outside the replaceable program directory, including portable layouts.
func DefaultRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Sidravia", "updates", "operations"), nil
}

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
			if exists && r.RequiresStartupBlock() {
				err = ErrConflict
			}
		} else if !exists || r.ID != id || r.Phase != Quiescent {
			err = ErrConflict
		}
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}
