//go:build linux

package transaction

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// AcquireInheritedStart returns a shared runtime lease for an exec-based GUI
// launcher. The caller owns Close and must deliberately clear FD_CLOEXEC only
// immediately before exec; the native runtime restores it before spawning kids.
func (s *Store) AcquireInheritedStart(ctx context.Context) (*os.File, error) {
	if _, _, err := s.Read(ctx); err != nil {
		return nil, err
	}
	file, err := lockFile(filepath.Join(s.directory, "execution.lock"), unix.LOCK_SH)
	if err != nil {
		return nil, err
	}
	r, exists, err := s.Read(ctx)
	if err == nil && (targetSealed(s.target) || exists && r.RequiresStartupBlock()) {
		err = ErrConflict
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
