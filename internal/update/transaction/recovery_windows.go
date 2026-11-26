//go:build windows

package transaction

import (
	"context"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"sidravia/internal/process/winproc"
	"strconv"
	"time"
)

// Windows keeps its own seal through cold recovery so the native GUI entry
// delegates to the same signed operation admission as Go entries.
func (s *Store) AcquireRecoveryStart(ctx context.Context, id, nonce, version, build, namespace, configuration string) (func(), error) {
	r, exists, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	if !exists || r.Source.Layout != "portable" || r.Phase != Resuming || r.ID != id || r.Recovery == nil || r.Target.Version != version || r.Target.BuildID != build {
		return nil, ErrConflict
	}
	rec := r.Recovery
	if rec.Nonce != nonce || rec.Namespace != namespace || rec.Intent.ConfigurationID != configuration || !time.Now().Before(rec.ExpiresAt) {
		return nil, ErrConflict
	}
	if err = VerifyPortableSeal(s.target, id); err != nil {
		return nil, ErrConflict
	}
	created, err := strconv.ParseUint(rec.CoordinatorStart, 10, 64)
	if err != nil {
		return nil, ErrInvalid
	}
	parent, err := winproc.ObserveParent()
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err = winproc.DescendsFrom(parent.PID, uint32(rec.CoordinatorPID), created); err != nil {
		return nil, err
	}
	return func() {}, nil
}

// VerifyPortableSeal is bounded and binds the same regular, single-link file
// before/open/after. It grants no phase or recovery permission on its own.
func VerifyPortableSeal(target, id string) error {
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	before, err := root.Lstat(StartupSeal)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() != int64(len(id)+1) || len(id) != 32 {
		return ErrInvalid
	}
	f, err := root.Open(StartupSeal)
	if err != nil {
		return err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, current) {
		return ErrInvalid
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrInvalid
	}
	b, err := io.ReadAll(io.LimitReader(f, 34))
	if err != nil {
		return err
	}
	if string(b) != id+"\n" {
		return ErrInvalid
	}
	after, err := root.Lstat(StartupSeal)
	if err != nil {
		return err
	}
	if !os.SameFile(current, after) {
		return ErrInvalid
	}
	return nil
}
