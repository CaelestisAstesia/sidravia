//go:build windows

package command

import (
	"context"
	"errors"
	"runtime"

	"sidravia/internal/productlayout"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
	"sidravia/internal/update/setup"
)

func applyPlatformSupported() bool { return runtime.GOARCH == "amd64" }
func validApplyTarget(l productlayout.Layout, kind string) bool {
	return l.Mode == productlayout.ModeInstalled && kind == "setup"
}
func platformApplyErrorCode(err error) string {
	switch {
	case errors.Is(err, setup.ErrHandoff):
		return "handoff_unavailable"
	case errors.Is(err, setup.ErrChanged):
		return "target_changed"
	case errors.Is(err, setup.ErrInvalid):
		return "invalid_metadata"
	case errors.Is(err, setup.ErrRecovery):
		return "resume_failed"
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
	r, err := setup.Begin(ctx, layout, version, build, old, oldAsset, index, next, c, path)
	if err != nil {
		return nil, err
	}
	return &applyResult{Operation: &operationView{ID: r.ID, Phase: r.Phase, SourceVersion: r.Source.Version, TargetVersion: r.Target.Version, UpdatedAt: r.UpdatedAt, RequiresStartupBlock: r.RequiresStartupBlock()}, HandedOver: true, ApplicationRecovery: "pending", Authentication: "not_evaluated"}, nil
}
func recoverOperation(ctx context.Context, layout productlayout.Layout, version, build, id string, _ *releases.GitHub) (*applyResult, error) {
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
