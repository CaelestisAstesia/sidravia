//go:build windows

package command

import (
	"context"
	"errors"
	"runtime"
	"sidravia/internal/update"
	"sidravia/internal/update/portable"

	"sidravia/internal/productlayout"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
	"sidravia/internal/update/setup"
)

func applyPlatformSupported() bool { return runtime.GOARCH == "amd64" }
func validApplyTarget(l productlayout.Layout, kind string) bool {
	return l.Mode == productlayout.ModeInstalled && kind == "setup" || l.Mode == productlayout.ModePortable && kind == defaultPackageKind(runtime.GOOS, runtime.GOARCH, l)
}
func platformApplyErrorCode(err error) string {
	var commitErr *portable.HandoffCommitError
	switch {
	case errors.As(err, &commitErr):
		return "handoff_unconfirmed"
	case errors.Is(err, portable.ErrHandoff), errors.Is(err, setup.ErrHandoff):
		return "handoff_unavailable"
	case errors.Is(err, portable.ErrChanged), errors.Is(err, setup.ErrChanged):
		return "target_changed"
	case errors.Is(err, portable.ErrInvalid), errors.Is(err, setup.ErrInvalid):
		return "invalid_metadata"
	case errors.Is(err, portable.ErrRestore), errors.Is(err, setup.ErrRecovery):
		return "resume_failed"
	case errors.Is(err, portable.ErrPreparation):
		return "preparation_rejected"
	case errors.Is(err, portable.ErrUnsupported):
		return "unsupported"
	}
	return "apply_failed"
}
func applyDownloaded(ctx context.Context, root string, layout productlayout.Layout, version, build string, source *releases.GitHub, c releases.Candidate, path string, _ func()) (*applyResult, error) {
	valid, err := inspectCached(root, path, &c)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, releases.ErrIntegrity
	}
	old, oldAsset, index, err := installedProof(ctx, source, layout, version, build)
	if err != nil {
		return nil, err
	}
	next, err := source.AuthenticateCandidate(ctx, c)
	if err != nil {
		return nil, err
	}
	var r operations.Record
	if layout.Mode == productlayout.ModePortable {
		r, err = portable.BeginWindows(ctx, layout, version, build, old, oldAsset, index, next, c, path)
	} else {
		r, err = setup.Begin(ctx, layout, version, build, old, oldAsset, index, next, c, path)
	}
	var commitErr *portable.HandoffCommitError
	if err != nil && !errors.As(err, &commitErr) {
		return nil, err
	}
	return &applyResult{Operation: &operationView{ID: r.ID, Phase: r.Phase, SourceVersion: r.Source.Version, TargetVersion: r.Target.Version, UpdatedAt: r.UpdatedAt, RequiresStartupBlock: r.RequiresStartupBlock()}, HandedOver: true, ApplicationRecovery: "pending", Authentication: "not_evaluated"}, err
}
func recoverOperation(ctx context.Context, layout productlayout.Layout, version, build, id string, source *releases.GitHub) (*applyResult, error) {
	if layout.Mode == productlayout.ModePortable {
		return recoverPortableWindows(ctx, layout, version, build, id, source)
	}
	err := setup.Reconcile(ctx, layout, version, build, id)
	root, e := operations.DefaultRoot()
	if e != nil {
		return nil, errors.Join(err, e)
	}
	store, e := operations.Open(root, layout.ExecutableDirectory, releases.PublisherTrust())
	if e != nil {
		return nil, errors.Join(err, e)
	}
	return operationResult(context.WithoutCancel(ctx), store), err
}

func recoverPortableWindows(ctx context.Context, layout productlayout.Layout, version, build, id string, source *releases.GitHub) (*applyResult, error) {
	if !update.ValidOperationID(id) {
		return nil, operations.ErrInvalid
	}
	root, err := operations.DefaultRoot()
	if err != nil {
		return nil, err
	}
	store, err := operations.Open(root, layout.ExecutableDirectory, releases.PublisherTrust())
	if err != nil {
		return nil, err
	}
	r, exists, err := store.Read(ctx)
	if err != nil {
		return nil, err
	}
	if !exists || r.ID != id {
		return nil, errOperation
	}
	result := func(err error) (*applyResult, error) { return operationResult(context.WithoutCancel(ctx), store), err }
	if r.Phase == operations.Applying || r.Phase == operations.RepairRequired {
		return result(errManualRepair)
	}
	expected := r.Source
	if r.Phase == operations.Applied || r.Phase == operations.Resuming || r.Phase == operations.Completed || r.Phase == operations.ResumeFailed {
		expected = r.Target
	}
	if version != expected.Version || build != expected.BuildID {
		return result(errSourceProof)
	}
	proof, c, index, err := installedProof(ctx, source, layout, version, build)
	if err != nil {
		return result(err)
	}
	if expected == r.Target {
		c.ID = r.Package.CandidateID
	}
	inventory, err := portable.Authenticate(proof, c, index)
	if err != nil {
		return result(err)
	}
	return result(portable.ReconcileInterrupted(ctx, store, id, layout.ExecutableDirectory, inventory))
}
