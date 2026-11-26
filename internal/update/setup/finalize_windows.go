//go:build windows

package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/document/jsondoc"
	"sidravia/internal/document/securefile"
	"sidravia/internal/installation/inno"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/winproc"
	"sidravia/internal/update/setup/internal/receiptstate"
	"sidravia/internal/update/transaction"
)

// Finish runs the newly installed updater after file publication. It retains an
// exact handle to its authenticated parent, then waits outside Inno's synchronous
// hooks. Its result never substitutes installer exit for a complete byte check.
func Finish(ctx context.Context, id string, pid uint32, identity daemonctl.Identity) error {
	layout, e := localLayout()
	if e != nil {
		return e
	}
	s, e := receiptstate.Read(ctx, layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	parent, e := boundParent(s, pid)
	if e != nil {
		return e
	}
	defer parent.Close()
	store, r, e := openOperation(ctx, layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	if r.Phase != transaction.Applied || r.Recovery == nil || identity.ProductVersion != r.Target.Version || identity.BuildID != r.Target.BuildID || s.Source != r.Source {
		return ErrInvalid
	}
	if e = claimFinalization(s.Source.ID, id); e != nil {
		return e
	}
	self, e := winproc.Observe(uint32(os.Getpid()))
	if e != nil {
		return e
	}
	s.Finalizer = receiptstate.ProcessID{self.PID, self.Created}
	self.Close()
	if e = saveSession(ctx, s); e != nil {
		return e
	}
	if e = parent.Wait(ctx); e != nil {
		return e
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	ctx = work
	code, e := parent.ExitCode()
	if e != nil {
		return e
	}
	s.InstallerExit = &code
	s.Integration = "not_confirmed"
	if code == 0 {
		s.Integration = "succeeded"
	}
	if code == 1 {
		s.Integration = "failed"
	}
	s.RestartRequired = code == 3010
	if e = saveSession(ctx, s); e != nil {
		return e
	}
	if e = verifyTarget(ctx, r, s); e != nil {
		return e
	}
	if code != 0 && code != 1 {
		return ErrRecovery
	}
	if _, changed, e := store.Advance(ctx, id, transaction.Applied, transaction.Resuming, "", time.Now()); e != nil || !changed {
		return errors.Join(transaction.ErrConflict, e)
	}
	root, e := productlayout.OperationRoot()
	if e != nil {
		return e
	}
	directory := filepath.Join(root, "setup", sum([]byte(strings.ToLower(r.Target.ID))), id+"-restore")
	secure, e := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if e != nil {
		return e
	}
	if _, _, e = secure.Read(ctx, filepath.Join(directory, "admission"), 16); e != nil {
		return e
	}
	recoveryErr := inno.RestoreInstalled(ctx, id, r.Target, r.Recovery.Intent, r.Recovery.ExpiresAt, r.Recovery.LogLevel, directory, identity, parent.SID, r.Recovery.Nonce)
	phase := transaction.Completed
	failure := ""
	if recoveryErr != nil {
		phase = transaction.ResumeFailed
		failure = "resume_failed"
	}
	_, changed, e := store.Advance(context.WithoutCancel(ctx), id, transaction.Resuming, phase, failure, time.Now())
	if e != nil || !changed {
		return errors.Join(recoveryErr, transaction.ErrConflict, e)
	}
	if e = removeSeal(r.Target.ID, id); e != nil {
		return e
	}
	if recoveryErr != nil {
		return errors.Join(ErrRecovery, recoveryErr)
	}
	return nil
}

func ownSeal(target, id string) (bool, error) {
	path := filepath.Join(target, transaction.StartupSeal)
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if !info.Mode().IsRegular() || info.Size() > 128 {
		return false, ErrChanged
	}
	if e = plainPath(path); e != nil {
		return false, e
	}
	f, e := os.Open(path)
	if e != nil {
		return false, e
	}
	defer f.Close()
	if e = singleLink(f); e != nil {
		return false, e
	}
	b, e := jsondoc.ReadLimited(f, 128)
	if e != nil {
		return false, e
	}
	if string(b) != "sidravia-update-v1\n"+id+"\n" {
		return false, ErrChanged
	}
	return true, nil
}
func removeSeal(target, id string) error {
	present, e := ownSeal(target, id)
	if e != nil {
		return e
	}
	if !present {
		return nil
	}
	return os.Remove(filepath.Join(target, transaction.StartupSeal))
}
