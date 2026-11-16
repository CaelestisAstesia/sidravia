//go:build !linux

package command

import (
	"context"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/releases"
)

func applyPlatformSupported() bool            { return false }
func platformApplyErrorCode(err error) string { return safeCode(err) }
func applyDownloaded(context.Context, string, productlayout.Layout, string, string, *releases.GitHub, releases.Candidate, string, func()) (*applyResult, error) {
	return nil, releases.ErrUnsupported
}
func recoverOperation(context.Context, productlayout.Layout, string, string, string, *releases.GitHub) (*applyResult, error) {
	return nil, releases.ErrUnsupported
}
