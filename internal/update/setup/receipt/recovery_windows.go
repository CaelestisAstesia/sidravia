package receipt

import (
	"context"

	"sidravia/internal/process/winproc"
	"sidravia/internal/update/setup/internal/receiptstate"
	"sidravia/internal/update/transaction/admission"
)

func AuthorizeRecovery(ctx context.Context, target, version, build string) error {
	id, source, err := admission.InstalledSource(ctx, target, admission.RecoveryFromEnvironment(version, build))
	if err != nil {
		return err
	}
	s, err := receiptstate.Read(ctx, target, id)
	if err != nil {
		return err
	}
	if s.Source != source || s.Finalizer.PID == 0 || s.Finalizer.Created == 0 {
		return admission.ErrConflict
	}
	parent, err := winproc.ObserveParent()
	if err != nil {
		return err
	}
	defer parent.Close()
	return winproc.DescendsFrom(parent.PID, s.Finalizer.PID, s.Finalizer.Created)
}
