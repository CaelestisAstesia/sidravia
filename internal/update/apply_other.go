//go:build !linux && !windows

package update

import (
	"context"
	"sidravia/internal/update/internal/request"

	"sidravia/internal/productlayout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
)

func platformApplyErrorCode(err error) string { return request.SourceCode(err) }
func applyDownloaded(context.Context, string, productlayout.Layout, string, string, *github.GitHub, release.Candidate, string, func()) (*applyResult, error) {
	return nil, release.ErrUnsupported
}
func recoverOperation(context.Context, productlayout.Layout, string, string, string, *github.GitHub) (*applyResult, error) {
	return nil, release.ErrUnsupported
}
