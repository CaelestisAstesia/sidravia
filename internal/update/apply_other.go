//go:build !windows

package update

import (
	"context"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/internal/request"
)

func platformApplyErrorCode(err error) string { return request.SourceCode(err) }
func applyDownloaded(context.Context, string, productlayout.Layout, string, string, *github.GitHub, release.Candidate, string) (*applyResult, error) {
	return nil, release.ErrUnsupported
}
