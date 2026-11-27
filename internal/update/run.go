// Package update hosts the shared updater on desktop. Application credentials
// and Windows installer mechanics do not belong in this entry point.
package update

import (
	"context"
	"io"
	"runtime"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/internal/request"
	"sidravia/internal/update/query"
)

func Run(ctx context.Context, args []string, version, buildID string, out io.Writer) int {
	emit := request.Writer(out)
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		return emit(true, "ok", "Sidravia updater: check | download --candidate ID | apply --candidate ID --confirm | status | recover --operation ID --confirm | version", nil)
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
	if operation != "download" && operation != "status" && operation != "apply" && operation != "recover" {
		return emit(false, "invalid_arguments", "未知更新操作。", nil)
	}
	options, argumentError := request.Parse(args[1:], version)
	if argumentError != "" {
		return emit(false, "invalid_arguments", argumentError, nil)
	}
	if (operation == "apply" || operation == "recover") && !options.Confirmed {
		return emit(false, "confirmation_required", "请检查候选或操作编号，并使用 --confirm 明确确认", nil)
	}
	if ((operation == "download" || operation == "apply") && options.CandidateID == "") || ((operation != "download" && operation != "apply") && options.CandidateID != "") || ((operation == "recover") != (options.OperationID != "")) || (options.Confirmed && operation != "apply" && operation != "recover") {
		return emit(false, "invalid_arguments", "更新参数或编号无效", nil)
	}
	layout, kind, _, failure := request.Resolve(options.Kind, false)
	if failure != nil {
		return failure.Write(out)
	}
	if operation == "recover" && release.QtRepositoryProfile(kind) != "" {
		return emit(false, "apply_unavailable", "请使用可信离线安装包修复此安装。", nil)
	}
	if operation == "apply" && (!request.ApplyPlatformSupported() || !release.PublisherTrust().Configured()) {
		return emit(false, "apply_unavailable", "当前平台或构建尚未具备可信自动更新条件，请按发行说明手动更新", nil)
	}
	// Setup recovery reconciles a local, signed execution record. Release
	// query cache availability and GitHub backoff are irrelevant here.
	if operation == "recover" && kind == "setup" && runtime.GOOS == "windows" && layout.Mode == productlayout.ModeInstalled {
		result, err := recoverOperation(ctx, layout, version, buildID, options.OperationID, nil)
		if err != nil {
			return emit(false, applyErrorCode(err), applyErrorMessage(err), result)
		}
		return emit(true, "ok", "中断协调已结束，未重复启动或恢复连接", result)
	}
	var execution *operationView
	if operation == "status" && kind == "setup" {
		operationRoot, err := productlayout.OperationRoot()
		if err != nil {
			return emit(false, "operation_status_unavailable", "更新执行记录无法确认，请检查更新状态后再操作。", nil)
		}
		execution, err = executionStatus(ctx, operationRoot, layout.ExecutableDirectory)
		if err != nil {
			return emit(false, "operation_status_unavailable", "更新执行记录无法确认，请检查更新状态后再操作。", nil)
		}
	}
	cacheFailure := func(code, message string) int {
		if execution != nil {
			return emit(true, "ok", message, composeStatus(candidatecache.State{}, false, execution))
		}
		return emit(false, code, message, nil)
	}
	selection := release.Selection{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: kind, Channel: options.Channel, CurrentVersion: version}
	store, failure := candidatecache.Open(layout.ExecutableDirectory, selection)
	if failure != nil {
		return cacheFailure(failure.Code, failure.Message)
	}
	previous, exists, failure := store.Read(ctx)
	if failure != nil {
		return cacheFailure(failure.Code, failure.Message)
	}
	if operation == "status" {
		previous = projectInstalledVersion(previous, version)
		if execution != nil {
			return emit(true, "ok", "", composeStatus(previous, exists, execution))
		}
		if release.QtRepositoryProfile(kind) != "" {
			outcome, present, err := readQtOutcome(ctx, store.Root(), version)
			if err != nil {
				return emit(false, "operation_status_unavailable", "无法确认维护工具结果，请检查当前版本。", nil)
			}
			if present {
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
			if outcome != nil && outcome.HandedOver {
				message := "安装器已接管，请在安装器中继续，完成后用 status 查询结果"
				if release.QtRepositoryProfile(kind) != "" {
					message = "维护工具正在继续安装更新，完成后可重新打开 Sidravia"
				}
				return request.Outcome{OK: true, Code: "handed_over", Message: message, Result: outcome}
			}
			return request.Outcome{OK: true, Code: "ok", Message: "更新已安装，恢复结果见返回状态", Result: outcome}
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
