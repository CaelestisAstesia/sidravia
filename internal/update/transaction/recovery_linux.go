//go:build linux

package transaction

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"sidravia/internal/process/linuxproc"
)

func (s *Store) recoveryAdmission(ctx context.Context, id, nonce, version, build, namespace, configuration string) (*os.File, error) {
	if _, _, err := s.Read(ctx); err != nil {
		return nil, err
	}
	file, err := lockFile(filepath.Join(s.directory, "execution.lock"), unix.LOCK_SH)
	if err != nil {
		return nil, err
	}
	reject := func(err error) (*os.File, error) { file.Close(); return nil, err }
	r, exists, err := s.Read(ctx)
	if err != nil {
		return reject(err)
	}
	if !exists || r.Phase != Resuming || r.ID != id || r.Recovery == nil || targetSealed(s.target) || r.Target.Version != version || r.Target.BuildID != build {
		return reject(ErrConflict)
	}
	recovery := r.Recovery
	if recovery.Nonce != nonce || namespace != recovery.Namespace || configuration != recovery.Intent.ConfigurationID || !time.Now().Before(recovery.ExpiresAt) {
		return reject(ErrConflict)
	}
	// A copied token in a terminal/sibling process is insufficient. Only the
	// exact live coordinator's descendants may form this cold-start tree.
	if err := linuxproc.DescendsFrom(os.Getppid(), recovery.CoordinatorPID, recovery.CoordinatorStart); err != nil {
		return reject(err)
	}
	return file, nil
}
func (s *Store) AcquireRecoveryStart(ctx context.Context, id, nonce, version, build, namespace, configuration string) (func(), error) {
	file, err := s.recoveryAdmission(ctx, id, nonce, version, build, namespace, configuration)
	if err != nil {
		return nil, err
	}
	return func() { _ = file.Close() }, nil
}
func (s *Store) AcquireInheritedRecoveryStart(ctx context.Context, id, nonce, version, build, namespace, configuration string) (*os.File, error) {
	return s.recoveryAdmission(ctx, id, nonce, version, build, namespace, configuration)
}
