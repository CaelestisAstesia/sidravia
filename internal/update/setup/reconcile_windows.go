//go:build windows

package setup

import (
	"context"
	"errors"
	"os"
	"time"

	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
	"sidravia/internal/update/windowsplatform"
)

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
