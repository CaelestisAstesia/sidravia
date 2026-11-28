//go:build windows

package update

import (
	"context"

	"sidravia/internal/background/daemonctl"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/request"

	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/internal/github"
)

func platformApplyErrorCode(error) string { return "apply_failed" }

func applyDownloaded(ctx context.Context, root string, layout productlayout.Layout, version, build string, source *github.GitHub, c release.Candidate, path string) (*applyResult, error) {
	valid, err := candidatecache.Inspect(root, path, &c)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, github.ErrIntegrity
	}
	next, err := source.AuthenticateCandidate(ctx, c)
	if err != nil {
		return nil, err
	}
	asset, err := next.Asset(c)
	if err != nil {
		return nil, err
	}
	if !request.ValidApplyTarget(layout, asset.Kind) {
		return nil, release.ErrUnsupported
	}
	return applyQt(ctx, root, layout, daemonctl.Identity{ProductVersion: version, BuildID: build}, c, next, path)
}
