// Package update hosts the shared updater on desktop. Application credentials
// and Windows installer mechanics do not belong in this entry point.
package update

import (
	"context"
	"io"
	"runtime"

	"sidravia/internal/installation/startgate"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/internal/request"
	"sidravia/internal/update/query"
)

func Run(ctx context.Context, args []string, version, buildID string, out io.Writer) int {
	emit := request.Writer(out)
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		return emit(true, "ok", "Sidravia updater: check | download --candidate ID | apply --candidate ID --confirm | status | version", nil)
	}
	if args[0] == "version" {
		if len(args) != 1 {
			return emit(false, "invalid_arguments", "更新参数无效。", nil)
		}
		return emit(true, "ok", "", map[string]string{"productVersion": version, "buildId": buildID})
	}
	operation := args[0]
	if operation == "check" {
		return query.Run(ctx, args[1:], version, out)
	}
	if operation != "download" && operation != "status" && operation != "apply" {
		return emit(false, "invalid_arguments", "未知更新操作。", nil)
	}
	options, argumentError := request.Parse(args[1:], version)
	if argumentError != "" {
		return emit(false, "invalid_arguments", argumentError, nil)
	}
	if operation == "apply" && !options.Confirmed {
		return emit(false, "confirmation_required", "请检查候选编号，并使用 --confirm 明确确认", nil)
	}
	if ((operation == "download" || operation == "apply") && options.CandidateID == "") || ((operation != "download" && operation != "apply") && options.CandidateID != "") || (options.Confirmed && operation != "apply") {
		return emit(false, "invalid_arguments", "更新参数或编号无效", nil)
	}
	layout, kind, _, failure := request.Resolve(options.Kind, false)
	if failure != nil {
		return failure.Write(out)
	}
	if operation == "apply" && (!request.ApplyPlatformSupported() || !release.PublisherTrust().Configured()) {
		return emit(false, "apply_unavailable", "当前平台或构建尚未具备可信自动更新条件，请按发行说明手动更新", nil)
	}
	selection := release.Selection{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: kind, Channel: options.Channel, CurrentVersion: version}
	store, failure := candidatecache.Open(layout.ExecutableDirectory, selection)
	if failure != nil {
		return failure.Write(out)
	}
	previous, exists, failure := store.Read(ctx)
	if failure != nil {
		return failure.Write(out)
	}
	if operation == "status" {
		previous = projectInstalledVersion(previous, version)
		if release.QtRepositoryProfile(kind) != "" {
			outcome, present, err := readQtOutcome(ctx, store.Root(), version)
			if err != nil {
				return emit(false, "operation_status_unavailable", "无法确认维护工具结果，请检查当前版本。", nil)
			}
			if present {
				if outcome.State == "pending" {
					unlock, err := startgate.AcquireStart()
					if err == nil {
						unlock()
						outcome.State = "unconfirmed"
					}
				}
				return emit(true, "ok", outcome.message(), qtStatusResult{State: previous, Installation: outcome})
			}
		}
		if !exists {
			return emit(true, "not_checked", "尚未检查更新。", nil)
		}
		return emit(true, "ok", "", previous)
	}
	return store.WithMutation(ctx, func(m candidatecache.Mutation) request.Outcome {
		source := github.NewGitHub(nil)
		state, outcome := query.Check(ctx, source, selection, layout, false, m)
		if !outcome.OK {
			return outcome
		}
		result := state.Check
		if result.Candidate == nil || result.Candidate.ID != options.CandidateID {
			return request.Fail("candidate_changed", "候选版本已变化，请重新检查。", nil)
		}

		if operation == "apply" {
			if !request.ValidApplyTarget(layout, kind) {
				return request.Fail("invalid_target", "仅支持保持现有包型的受支持安装目标", nil)
			}
			if result.State != "available" || result.Candidate == nil {
				return request.Fail("not_newer", "候选版本不高于当前版本", nil)
			}
			path, oldCandidate := m.Downloaded()
			if path == "" || !candidatecache.SamePackage(oldCandidate, result.Candidate) {
				return request.Fail("download_required", "请先下载当前准确候选，再确认更新", nil)
			}
			outcome, err := applyDownloaded(ctx, m.Root(), layout, version, buildID, source, *result.Candidate, path)
			if err != nil {
				return m.SourceFailure(ctx, err, applyErrorCode(err), applyErrorMessage(err), outcome)
			}
			return request.Outcome{OK: true, Code: "handed_over", Message: "维护工具正在继续安装更新，完成后可重新打开 Sidravia", Result: outcome}
		}
		newDownload := ""
		if operation == "download" {
			if release.QtRepositoryProfile(kind) != "" && !result.PublisherVerified {
				return request.Fail("publisher_signature_required", "更新仓库缺少可信发行签名。", nil)
			}
			if state.Downloaded == "" {
				path, err := github.Download(ctx, nil, *result.Candidate, m.Root())
				if err != nil {
					return m.SourceFailure(ctx, err, request.SourceCode(err), request.SourceMessage(err), nil)
				}
				state.Downloaded = path
				newDownload = path
			}
		}
		return m.Commit(ctx, state, newDownload)
	}).Write(out)
}
