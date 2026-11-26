// Package query implements release checks without loading the update executors.
package query

import (
	"context"
	"io"
	"runtime"
	"time"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/internal/request"
)

// Run handles the arguments following check for both CTL and updater.
func Run(ctx context.Context, args []string, version string, out io.Writer) int {
	options, argumentError := request.Parse(args, version)
	if argumentError != "" {
		return request.Fail("invalid_arguments", argumentError, nil).Write(out)
	}
	if options.CandidateID != "" || options.OperationID != "" || options.Confirmed {
		return request.Fail("invalid_arguments", "更新参数或编号无效", nil).Write(out)
	}
	layout, kind, rpm, failure := request.Resolve(options.Kind, true)
	if failure != nil {
		return failure.Write(out)
	}
	selection := release.Selection{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: kind, Channel: options.Channel, CurrentVersion: version}
	store, failure := candidatecache.Open(layout.ExecutableDirectory, selection)
	if failure != nil {
		return failure.Write(out)
	}
	if _, _, failure := store.Read(ctx); failure != nil {
		return failure.Write(out)
	}
	return store.WithMutation(ctx, func(m candidatecache.Mutation) request.Outcome {
		state, outcome := Check(ctx, github.NewGitHub(nil), selection, layout, rpm, m)
		if !outcome.OK || rpm {
			return outcome
		}
		return m.Commit(ctx, state, "")
	}).Write(out)
}

// Check shares selection and cache projection with updater's download/apply
// path. It runs while the caller owns the candidate cache mutation.
func Check(ctx context.Context, source *github.GitHub, selection release.Selection, layout productlayout.Layout, rpm bool, m candidatecache.Mutation) (candidatecache.State, request.Outcome) {
	result, err := source.Check(ctx, selection)
	if err != nil {
		return candidatecache.State{}, m.SourceFailure(ctx, err, request.SourceCode(err), request.SourceMessage(err), nil)
	}
	state := candidatecache.State{SchemaVersion: 1, Selection: selection, CheckedAt: time.Now().UTC(), Check: result}
	if rpm {
		if result.Candidate != nil {
			result.Candidate.ApplyReason = "system_package_managed"
		}
		return state, request.Success(state)
	}
	ProjectApplyOffer(result, request.ValidApplyTarget(layout, selection.Kind), request.ApplyPlatformSupported())
	if failure := m.PreserveDownload(&state); failure != nil {
		return candidatecache.State{}, request.Fail(failure.Code, failure.Message, failure.Result)
	}
	return state, request.Success(state)
}
