//go:build windows

package setup

import (
	"context"
	"errors"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/process/winproc"
	"sidravia/internal/release"
	"sidravia/internal/update/setup/internal/receiptstate"
	"sidravia/internal/update/transaction"
)

func Reconcile(ctx context.Context, layout productlayout.Layout, version, build, id string) error {
	store, r, e := openOperation(ctx, layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	s, e := receiptstate.Read(ctx, layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	if s.Source != r.Source {
		return ErrInvalid
	}
	for _, owner := range []receiptstate.ProcessID{s.Finalizer, s.Coordinator, s.Updater} {
		if owner.PID == 0 {
			continue
		}
		running, e := winproc.GenerationRunning(owner.PID, owner.Created)
		if e != nil {
			return e
		}
		if running {
			return transaction.ErrConflict
		}
	}
	sealed, e := ownSeal(layout.ExecutableDirectory, id)
	if e != nil {
		return e
	}
	next := transaction.Aborted
	code := "preparation_failed"
	targetPhase := r.Phase == transaction.Applied || r.Phase == transaction.Resuming || r.Phase == transaction.Completed || r.Phase == transaction.ResumeFailed || r.Phase == transaction.Applying || r.Phase == transaction.RepairRequired
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
		if r.Phase == transaction.Applying || r.Phase == transaction.RepairRequired {
			// RepairRequired is deliberately not auto-transitioned: a manual installer
			// must establish its exact target first; this reconciliation is explicit.
			if r.Phase == transaction.RepairRequired {
				return ErrChanged
			}
			if _, changed, e := store.Advance(ctx, id, r.Phase, transaction.Applied, "", time.Now()); e != nil || !changed {
				return errors.Join(transaction.ErrConflict, e)
			}
			r.Phase = transaction.Applied
		}
		if r.Phase == transaction.Completed || r.Phase == transaction.ResumeFailed {
			if sealed {
				return removeSeal(r.Target.ID, id)
			}
			return nil
		}
		next = transaction.ResumeFailed
		code = "resume_failed"
	} else if r.Phase == transaction.Created || r.Phase == transaction.Verified || r.Phase == transaction.Preparing || r.Phase == transaction.Quiescent || r.Phase == transaction.Aborted {
		if version != r.Source.Version || build != r.Source.BuildID {
			return ErrInvalid
		}
		proof, c, e := s.SourceProof(release.PublisherTrust())
		if e != nil {
			return e
		}
		if _, e = VerifyInstalled(ctx, r.Source.ID, proof, c, s.SourceIndex, s.HasGUI); e != nil {
			return e
		}
		if r.Phase == transaction.Aborted {
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
		return transaction.ErrConflict
	}
	if sealed {
		return removeSeal(r.Source.ID, id)
	}
	return nil
}
