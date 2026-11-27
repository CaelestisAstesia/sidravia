//go:build windows

package update

import (
	"context"
	"errors"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"

	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/setup"
	"sidravia/internal/update/transaction"
)

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
func applyDownloaded(ctx context.Context, root string, layout productlayout.Layout, version, build string, source *github.GitHub, c release.Candidate, path string) (*applyResult, error) {
	valid, err := candidatecache.Inspect(root, path, &c)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, github.ErrIntegrity
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
	return &applyResult{Operation: &operationView{ID: r.ID, Phase: r.Phase, SourceVersion: r.Source.Version, TargetVersion: r.Target.Version, UpdatedAt: r.UpdatedAt, RequiresStartupBlock: r.RequiresStartupBlock()}, HandedOver: true, ApplicationRecovery: "pending", Authentication: "not_evaluated"}, err
}
func recoverOperation(ctx context.Context, layout productlayout.Layout, version, build, id string, source *github.GitHub) (*applyResult, error) {
	err := setup.Reconcile(ctx, layout, version, build, id)
	root, e := productlayout.OperationRoot()
	if e != nil {
		return nil, errors.Join(err, e)
	}
	store, e := transaction.Open(root, layout.ExecutableDirectory, release.PublisherTrust())
	if e != nil {
		return nil, errors.Join(err, e)
	}
	return operationResult(context.WithoutCancel(ctx), store), err
}
