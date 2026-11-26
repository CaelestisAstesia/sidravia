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
	"time"

	"sidravia/internal/background/daemonctl"
	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/document/jsondoc"
	"sidravia/internal/installation/change"
	"sidravia/internal/installation/inno"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/winproc"
	"sidravia/internal/release"
	"sidravia/internal/update/setup/internal/innopeer"
	"sidravia/internal/update/setup/internal/receiptstate"
	"sidravia/internal/update/transaction"
)

func openOperation(ctx context.Context, target, id string) (*transaction.Store, transaction.Record, error) {
	root, e := productlayout.OperationRoot()
	if e != nil {
		return nil, transaction.Record{}, e
	}
	store, e := transaction.Open(root, target, release.PublisherTrust())
	if e != nil {
		return nil, transaction.Record{}, e
	}
	r, exists, e := store.Read(ctx)
	if e != nil {
		return nil, r, e
	}
	if !exists || r.ID != id || r.Source.Layout != "installed" {
		return nil, r, transaction.ErrConflict
	}
	return store, r, nil
}
func boundParent(s receiptstate.Session, pid uint32) (*winproc.Process, error) {
	parent, e := winproc.ObserveParent()
	if e != nil {
		return nil, e
	}
	if parent.PID != pid || parent.PID != s.Coordinator.PID || parent.Created != s.Coordinator.Created || !parent.Running() {
		parent.Close()
		return nil, winproc.ErrPeer
	}
	self, e := winproc.Observe(uint32(os.Getpid()))
	if e != nil {
		parent.Close()
		return nil, e
	}
	defer self.Close()
	if self.SID != parent.SID || self.Integrity != parent.Integrity {
		parent.Close()
		return nil, winproc.ErrPeer
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
func RunStage(ctx context.Context, stage, id string, coordinator, updater uint32, hasGUI bool, identity daemonctl.Identity) error {
	if !change.ValidID(id) {
		return ErrInvalid
	}
	layout, e := localLayout()
	if e != nil {
		return e
	}
	target := layout.ExecutableDirectory
	s, e := receiptstate.Read(ctx, target, id)
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
		parent, e := winproc.ObserveParent()
		if e != nil {
			return e
		}
		defer parent.Close()
		if parent.PID != coordinator || updater != s.Updater.PID || s.Coordinator.PID != 0 || r.Phase != transaction.Verified || identity.ProductVersion != r.Source.Version || identity.BuildID != r.Source.BuildID {
			return ErrInvalid
		}
		source, c, e := s.SourceProof(release.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, source, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		return innopeer.Acknowledge(ctx, id, updater, coordinator)
	}
	parent, e := boundParent(s, coordinator)
	if e != nil {
		return e
	}
	defer parent.Close()
	advance := func(from, to transaction.Phase, code string) error {
		_, changed, e := store.Advance(ctx, id, from, to, code, time.Now())
		if e != nil {
			return e
		}
		if !changed {
			return transaction.ErrConflict
		}
		return nil
	}
	if stage == "ready" {
		if identity.ProductVersion != r.Target.Version || identity.BuildID != r.Target.BuildID || r.Phase != transaction.Applied {
			return ErrInvalid
		}
		for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
			current, e := receiptstate.Read(ctx, target, id)
			if e != nil {
				return e
			}
			if current.Finalizer.PID != 0 {
				if e := winproc.DescendsFrom(current.Finalizer.PID, parent.PID, parent.Created); e != nil {
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
		if !oldIdentity || r.Phase != transaction.Verified || s.HasGUI != hasGUI {
			return ErrInvalid
		}
		if running, e := winproc.GenerationRunning(s.Updater.PID, s.Updater.Created); e != nil || running {
			return errors.Join(transaction.ErrConflict, e)
		}
		proof, c, e := s.SourceProof(release.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		if e = inno.Run(ctx, "prepare", id, coordinator, false, identity); e != nil {
			return e
		}
		intent, level, e := inno.PreparedSnapshot(ctx, id, coordinator, identity)
		if e != nil {
			return e
		}
		nonce := make([]byte, 16)
		if _, e = rand.Read(nonce); e != nil {
			return e
		}
		rec := transaction.Recovery{SourceFilesSHA256: sum(s.SourceIndex), Intent: intent, Namespace: s.Namespace, CoordinatorPID: int(parent.PID), CoordinatorStart: strconv.FormatUint(parent.Created, 10), Nonce: hex.EncodeToString(nonce), ExpiresAt: time.Now().Add(25 * time.Minute), LogLevel: level}
		if e = store.SetRecovery(ctx, id, rec, time.Now()); e != nil {
			return e
		}
		return advance(transaction.Verified, transaction.Preparing, "")
	}
	if stage == "stop" {
		if !oldIdentity || r.Phase != transaction.Preparing {
			return ErrInvalid
		}
		if e = inno.Run(ctx, "stop", id, coordinator, false, identity); e != nil {
			return e
		}
		return advance(transaction.Preparing, transaction.Quiescent, "")
	}
	if stage == "write" {
		if !oldIdentity || r.Phase != transaction.Quiescent {
			return ErrInvalid
		}
		proof, c, e := s.SourceProof(release.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		// The seal stays in the target even if the operation cache is moved/removed.
		seal, e := os.OpenFile(filepath.Join(target, transaction.StartupSeal), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return e
		}
		_, writeErr := seal.WriteString("sidravia-update-v1\n" + id + "\n")
		syncErr := seal.Sync()
		closeErr := seal.Close()
		if e = errors.Join(writeErr, syncErr, closeErr); e != nil {
			return e
		}
		return advance(transaction.Quiescent, transaction.Applying, "")
	}
	if stage == "installed" {
		if !newIdentity || r.Phase != transaction.Applying || s.HasGUI != hasGUI {
			return ErrInvalid
		}
		if e = retireInstalled(ctx, r, s); e != nil {
			return e
		}
		if e = verifyTarget(ctx, r, s); e != nil {
			return e
		}
		if e = advance(transaction.Applying, transaction.Applied, ""); e != nil {
			return e
		}
		return nil
	}
	if stage == "abort" {
		if !oldIdentity || (r.Phase != transaction.Verified && r.Phase != transaction.Preparing && r.Phase != transaction.Quiescent) {
			return ErrInvalid
		}
		if _, e = os.Lstat(filepath.Join(target, transaction.StartupSeal)); !os.IsNotExist(e) {
			return ErrChanged
		}
		proof, c, e := s.SourceProof(release.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, target, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		if e = advance(r.Phase, transaction.Aborted, "preparation_failed"); e != nil {
			return e
		}
		return inno.Run(ctx, "abort", id, coordinator, false, identity)
	}
	return ErrInvalid
}
func targetEvidence(r transaction.Record) (release.VerifiedRelease, release.Candidate, []byte, error) {
	proof, e := release.PublisherTrust().Verify(r.Manifest, r.Signature, r.Target.Version)
	c := release.Candidate{ID: r.Package.CandidateID, Version: r.Target.Version, Name: r.Package.Name, Size: r.Package.Size, SHA256: r.Package.SHA256}
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
	b, e := jsondoc.ReadLimited(f, 2<<20)
	closeErr := f.Close()
	return proof, c, b, errors.Join(e, closeErr)
}
func verifyTarget(ctx context.Context, r transaction.Record, s receiptstate.Session) error {
	proof, c, index, e := targetEvidence(r)
	if e != nil {
		return e
	}
	_, e = VerifyInstalled(ctx, r.Target.ID, proof, c, index, s.HasGUI)
	return e
}
