//go:build windows

package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/launchcontract"
	"sidravia/internal/persistence/jsonfile"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
	"sidravia/internal/update/lifecycle"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
	"sidravia/internal/update/windowsplatform"
)

func openOperation(ctx context.Context, target, id string) (*operations.Store, operations.Record, error) {
	root, e := operations.DefaultRoot()
	if e != nil {
		return nil, operations.Record{}, e
	}
	store, e := operations.Open(root, target, releases.PublisherTrust())
	if e != nil {
		return nil, operations.Record{}, e
	}
	r, exists, e := store.Read(ctx)
	if e != nil {
		return nil, r, e
	}
	if !exists || r.ID != id || r.Source.Layout != "installed" {
		return nil, r, operations.ErrConflict
	}
	return store, r, nil
}
func boundParent(s Session, pid uint32) (*windowsplatform.Process, error) {
	parent, e := windowsplatform.ObserveParent()
	if e != nil {
		return nil, e
	}
	if parent.PID != pid || parent.PID != s.Coordinator.PID || parent.Created != s.Coordinator.Created || !parent.Running() {
		parent.Close()
		return nil, windowsplatform.ErrPeer
	}
	self, e := windowsplatform.Observe(uint32(os.Getpid()))
	if e != nil {
		parent.Close()
		return nil, e
	}
	defer self.Close()
	if self.SID != parent.SID || self.Integrity != parent.Integrity {
		parent.Close()
		return nil, windowsplatform.ErrPeer
	}
	return parent, nil
}
func localLayout() (productlayout.Layout, error) {
	ns, e := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if e != nil {
		return productlayout.Layout{}, e
	}
	return productlayout.ResolveNamespace(ns)
}

// RunStage is a bounded direct child of the authenticated Inno worker. External
// flags cannot authorize a different target or turn a normal installer into an
// update. Both the immutable signed operation and the exact worker must match.
func RunStage(ctx context.Context, stage, id string, coordinator, updater uint32, hasGUI bool, identity clientbootstrap.Identity) error {
	if !update.ValidOperationID(id) {
		return ErrInvalid
	}
	layout, e := localLayout()
	if e != nil {
		return e
	}
	target := layout.ExecutableDirectory
	s, e := ReadSession(ctx, target, id)
	if e != nil {
		return e
	}
	if layout.Mode != productlayout.ModeInstalled || s.Namespace != os.Getenv(launchcontract.EnvNamespace) || !time.Now().Before(s.Expires) {
		return ErrInvalid
	}
	store, r, e := openOperation(ctx, target, id)
	if e != nil {
		return e
	}
	if s.Source != r.Source {
		return ErrInvalid
	}
	if stage == "accept" {
		parent, e := windowsplatform.ObserveParent()
		if e != nil {
			return e
		}
		defer parent.Close()
		if parent.PID != coordinator || updater != s.Updater.PID || s.Coordinator.PID != 0 || r.Phase != operations.Verified || identity.ProductVersion != r.Source.Version || identity.BuildID != r.Source.BuildID {
			return ErrInvalid
		}
		source, c, e := s.SourceProof(releases.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, source, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		return windowsplatform.Acknowledge(ctx, id, updater, coordinator)
	}
	parent, e := boundParent(s, coordinator)
	if e != nil {
		return e
	}
	defer parent.Close()
	advance := func(from, to operations.Phase, code string) error {
		_, changed, e := store.Advance(ctx, id, from, to, code, time.Now())
		if e != nil {
			return e
		}
		if !changed {
			return operations.ErrConflict
		}
		return nil
	}
	if stage == "ready" {
		if identity.ProductVersion != r.Target.Version || identity.BuildID != r.Target.BuildID || r.Phase != operations.Applied {
			return ErrInvalid
		}
		for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
			current, e := ReadSession(ctx, target, id)
			if e != nil {
				return e
			}
			if current.Finalizer.PID != 0 {
				if e := windowsplatform.DescendsFrom(current.Finalizer.PID, parent.PID, parent.Created); e != nil {
					return e
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		return ErrRecovery
	}
	oldIdentity := identity.ProductVersion == r.Source.Version && identity.BuildID == r.Source.BuildID
	newIdentity := identity.ProductVersion == r.Target.Version && identity.BuildID == r.Target.BuildID
	if stage == "prepare" {
		if !oldIdentity || r.Phase != operations.Verified || s.HasGUI != hasGUI {
			return ErrInvalid
		}
		if running, e := windowsplatform.GenerationRunning(s.Updater.PID, s.Updater.Created); e != nil || running {
			return errors.Join(operations.ErrConflict, e)
		}
		proof, c, e := s.SourceProof(releases.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		if e = lifecycle.Run(ctx, "prepare", id, coordinator, false, identity); e != nil {
			return e
		}
		intent, level, e := lifecycle.PreparedSnapshot(ctx, id, coordinator, identity)
		if e != nil {
			return e
		}
		nonce := make([]byte, 16)
		if _, e = rand.Read(nonce); e != nil {
			return e
		}
		rec := operations.Recovery{SourceFilesSHA256: sum(s.SourceIndex), Intent: intent, Namespace: s.Namespace, CoordinatorPID: int(parent.PID), CoordinatorStart: strconv.FormatUint(parent.Created, 10), Nonce: hex.EncodeToString(nonce), ExpiresAt: time.Now().Add(25 * time.Minute), LogLevel: level}
		if e = store.SetRecovery(ctx, id, rec, time.Now()); e != nil {
			return e
		}
		return advance(operations.Verified, operations.Preparing, "")
	}
	if stage == "stop" {
		if !oldIdentity || r.Phase != operations.Preparing {
			return ErrInvalid
		}
		if e = lifecycle.Run(ctx, "stop", id, coordinator, false, identity); e != nil {
			return e
		}
		return advance(operations.Preparing, operations.Quiescent, "")
	}
	if stage == "write" {
		if !oldIdentity || r.Phase != operations.Quiescent {
			return ErrInvalid
		}
		proof, c, e := s.SourceProof(releases.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		// The seal stays in the target even if the operation cache is moved/removed.
		seal, e := os.OpenFile(filepath.Join(target, operations.StartupSeal), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return e
		}
		_, writeErr := seal.WriteString("sidravia-update-v1\n" + id + "\n")
		syncErr := seal.Sync()
		closeErr := seal.Close()
		if e = errors.Join(writeErr, syncErr, closeErr); e != nil {
			return e
		}
		return advance(operations.Quiescent, operations.Applying, "")
	}
	if stage == "installed" {
		if !newIdentity || r.Phase != operations.Applying || s.HasGUI != hasGUI {
			return ErrInvalid
		}
		if e = retireInstalled(ctx, r, s); e != nil {
			return e
		}
		if e = verifyTarget(ctx, r, s); e != nil {
			return e
		}
		if e = advance(operations.Applying, operations.Applied, ""); e != nil {
			return e
		}
		return nil
	}
	if stage == "abort" {
		if !oldIdentity || (r.Phase != operations.Verified && r.Phase != operations.Preparing && r.Phase != operations.Quiescent) {
			return ErrInvalid
		}
		if _, e = os.Lstat(filepath.Join(target, operations.StartupSeal)); !os.IsNotExist(e) {
			return ErrChanged
		}
		proof, c, e := s.SourceProof(releases.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		if e = advance(r.Phase, operations.Aborted, "preparation_failed"); e != nil {
			return e
		}
		return lifecycle.Run(ctx, "abort", id, coordinator, false, identity)
	}
	return ErrInvalid
}
func targetEvidence(r operations.Record) (releases.VerifiedRelease, releases.Candidate, []byte, error) {
	proof, e := releases.PublisherTrust().Verify(r.Manifest, r.Signature, r.Target.Version)
	c := releases.Candidate{ID: r.Package.CandidateID, Version: r.Target.Version, Name: r.Package.Name, Size: r.Package.Size, SHA256: r.Package.SHA256}
	if e != nil {
		return proof, c, nil, e
	}
	root, e := os.OpenRoot(r.Target.ID)
	if e != nil {
		return proof, c, nil, e
	}
	defer root.Close()
	if e = plainPath(filepath.Join(r.Target.ID, "sidravia-files.json")); e != nil {
		return proof, c, nil, e
	}
	f, e := root.Open("sidravia-files.json")
	if e != nil {
		return proof, c, nil, e
	}
	if e = singleLink(f); e != nil {
		f.Close()
		return proof, c, nil, e
	}
	b, e := jsonfile.ReadLimited(f, 2<<20)
	closeErr := f.Close()
	return proof, c, b, errors.Join(e, closeErr)
}
func verifyTarget(ctx context.Context, r operations.Record, s Session) error {
	proof, c, index, e := targetEvidence(r)
	if e != nil {
		return e
	}
	_, e = VerifyInstalled(ctx, r.Target.ID, proof, c, index, s.HasGUI)
	return e
}

// Finish runs the newly installed updater after file publication. It retains an
// exact handle to its authenticated parent, then waits outside Inno's synchronous
// hooks. Its result never substitutes installer exit for a complete byte check.
func Finish(ctx context.Context, id string, pid uint32, identity clientbootstrap.Identity) error {
	layout, e := localLayout()
	if e != nil {
		return e
	}
	s, e := ReadSession(ctx, layout.ExecutableDirectory, id)
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
	if r.Phase != operations.Applied || r.Recovery == nil || identity.ProductVersion != r.Target.Version || identity.BuildID != r.Target.BuildID || s.Source != r.Source {
		return ErrInvalid
	}
	claimPath, e := sessionPath(s.Source.ID, id)
	if e != nil {
		return e
	}
	claim, e := os.OpenFile(claimPath+".finish", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	if e = claim.Close(); e != nil {
		return e
	}
	self, e := windowsplatform.Observe(uint32(os.Getpid()))
	if e != nil {
		return e
	}
	s.Finalizer = ProcessID{self.PID, self.Created}
	self.Close()
	if e = SaveSession(ctx, s); e != nil {
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
	if e = SaveSession(ctx, s); e != nil {
		return e
	}
	if e = verifyTarget(ctx, r, s); e != nil {
		return e
	}
	if code != 0 && code != 1 {
		return ErrRecovery
	}
	if _, changed, e := store.Advance(ctx, id, operations.Applied, operations.Resuming, "", time.Now()); e != nil || !changed {
		return errors.Join(operations.ErrConflict, e)
	}
	root, e := operations.DefaultRoot()
	if e != nil {
		return e
	}
	directory := filepath.Join(root, "setup", sum([]byte(strings.ToLower(r.Target.ID))), id+"-restore")
	secure, e := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if e != nil {
		return e
	}
	if _, _, e = secure.Read(ctx, filepath.Join(directory, "admission"), 16); e != nil {
		return e
	}
	recoveryErr := lifecycle.RestoreInstalled(ctx, id, r.Target, r.Recovery.Intent, r.Recovery.ExpiresAt, r.Recovery.LogLevel, directory, identity, parent.SID, r.Recovery.Nonce)
	phase := operations.Completed
	failure := ""
	if recoveryErr != nil {
		phase = operations.ResumeFailed
		failure = "resume_failed"
	}
	_, changed, e := store.Advance(context.WithoutCancel(ctx), id, operations.Resuming, phase, failure, time.Now())
	if e != nil || !changed {
		return errors.Join(recoveryErr, operations.ErrConflict, e)
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
	path := filepath.Join(target, operations.StartupSeal)
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
	b, e := jsonfile.ReadLimited(f, 128)
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
	return os.Remove(filepath.Join(target, operations.StartupSeal))
}
func Reconcile(ctx context.Context, layout productlayout.Layout, version, build, id string) error {
	store, r, e := openOperation(ctx, layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	s, e := ReadSession(ctx, layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	if s.Source != r.Source {
		return ErrInvalid
	}
	for _, owner := range []ProcessID{s.Finalizer, s.Coordinator, s.Updater} {
		if owner.PID == 0 {
			continue
		}
		running, e := windowsplatform.GenerationRunning(owner.PID, owner.Created)
		if e != nil {
			return e
		}
		if running {
			return operations.ErrConflict
		}
	}
	sealed, e := ownSeal(layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	next := operations.Aborted
	code := "preparation_failed"
	targetPhase := r.Phase == operations.Applied || r.Phase == operations.Resuming || r.Phase == operations.Completed || r.Phase == operations.ResumeFailed || r.Phase == operations.Applying || r.Phase == operations.RepairRequired
	if targetPhase {
		if version != r.Target.Version || build != r.Target.BuildID {
			return ErrInvalid
		}
		if e = retireInstalled(ctx, r, s); e != nil {
			return e
		}
		// A partial tree cannot pass this check. After an explicit manual repair to
		// the exact signed target, the same check can safely release its own seal.
		if e = verifyTarget(ctx, r, s); e != nil {
			return e
		}
		if r.Phase == operations.Applying || r.Phase == operations.RepairRequired {
			// RepairRequired is deliberately not auto-transitioned: a manual installer
			// must establish its exact target first; this reconciliation is explicit.
			if r.Phase == operations.RepairRequired {
				return ErrChanged
			}
			if _, changed, e := store.Advance(ctx, id, r.Phase, operations.Applied, "", time.Now()); e != nil || !changed {
				return errors.Join(operations.ErrConflict, e)
			}
			r.Phase = operations.Applied
		}
		if r.Phase == operations.Completed || r.Phase == operations.ResumeFailed {
			if sealed {
				return removeSeal(r.Target.ID, id)
			}
			return nil
		}
		next = operations.ResumeFailed
		code = "resume_failed"
	} else if r.Phase == operations.Created || r.Phase == operations.Verified || r.Phase == operations.Preparing || r.Phase == operations.Quiescent || r.Phase == operations.Aborted {
		if version != r.Source.Version || build != r.Source.BuildID {
			return ErrInvalid
		}
		proof, c, e := s.SourceProof(releases.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, r.Source.ID, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		if r.Phase == operations.Aborted {
			if sealed {
				return removeSeal(r.Source.ID, id)
			}
			return nil
		}
	} else {
		return ErrChanged
	}
	_, changed, e := store.Advance(ctx, id, r.Phase, next, code, time.Now())
	if e != nil {
		return e
	}
	if !changed {
		return operations.ErrConflict
	}
	if sealed {
		return removeSeal(r.Source.ID, id)
	}
	return nil
}

func AuthorizeRecovery(ctx context.Context, target, version, build string) error {
	id, token := os.Getenv(launchcontract.EnvRecoveryOperation), os.Getenv(launchcontract.EnvRecoveryToken)
	store, r, e := openOperation(ctx, target, id)
	_ = store
	if e != nil {
		return e
	}
	rec := r.Recovery
	if r.Phase != operations.Resuming || rec == nil || token != rec.Nonce || r.Target.Version != version || r.Target.BuildID != build || rec.Namespace != os.Getenv(launchcontract.EnvNamespace) || rec.Intent.ConfigurationID != os.Getenv(launchcontract.EnvRecoveryConfiguration) || !time.Now().Before(rec.ExpiresAt) {
		return operations.ErrConflict
	}
	s, e := ReadSession(ctx, target, id)
	if e != nil {
		return e
	}
	if s.Source != r.Source || s.Finalizer.PID == 0 || s.Finalizer.Created == 0 {
		return operations.ErrConflict
	}
	parent, e := windowsplatform.ObserveParent()
	if e != nil {
		return e
	}
	defer parent.Close()
	return windowsplatform.DescendsFrom(parent.PID, s.Finalizer.PID, s.Finalizer.Created)
}
