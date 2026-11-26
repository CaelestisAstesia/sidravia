//go:build !linux && !windows

package update

import (
	"context"

	"sidravia/internal/productlayout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
)

func applyPlatformSupported() bool            { return false }
func platformApplyErrorCode(err error) string { return safeCode(err) }
func applyDownloaded(context.Context, string, productlayout.Layout, string, string, *github.GitHub, release.Candidate, string, func()) (*applyResult, error) {
	return nil, release.ErrUnsupported
}
func recoverOperation(context.Context, productlayout.Layout, string, string, string, *github.GitHub) (*applyResult, error) {
	return nil, release.ErrUnsupported
}

func validApplyTarget(productlayout.Layout, string) bool { return false }
