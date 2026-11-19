//go:build !linux && !windows

package command

import (
	"context"

	"sidravia/internal/productlayout"
	"sidravia/internal/update/github"
	"sidravia/internal/update/releases"
)

func applyPlatformSupported() bool            { return false }
func platformApplyErrorCode(err error) string { return safeCode(err) }
func applyDownloaded(context.Context, string, productlayout.Layout, string, string, *github.GitHub, releases.Candidate, string, func()) (*applyResult, error) {
	return nil, releases.ErrUnsupported
}
func recoverOperation(context.Context, productlayout.Layout, string, string, string, *github.GitHub) (*applyResult, error) {
	return nil, releases.ErrUnsupported
}

func validApplyTarget(productlayout.Layout, string) bool { return false }
