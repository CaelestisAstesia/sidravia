// Package command hosts the shared updater on desktop. Application credentials
// and Windows installer mechanics do not belong in this entry point.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"time"

	"sidravia/internal/persistence/jsonfile"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/github"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
)

func Run(ctx context.Context, args []string, version, buildID string, out io.Writer) int {
	return RunWithAdmission(ctx, args, version, buildID, out, nil)
}

// RunWithAdmission transfers the host's own runtime lease only after the
// coordinator has durably blocked ordinary startup. Read-only commands need no lease.
func RunWithAdmission(ctx context.Context, args []string, version, buildID string, out io.Writer, releaseRuntime func()) int {
	emit := responseWriter(out)
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
	if operation != "check" && operation != "download" && operation != "status" && operation != "apply" && operation != "recover" {
		return emit(false, "invalid_arguments", "未知更新操作。", nil)
	}
	options, argumentError := parseArguments(args[1:], version)
	if argumentError != "" {
		return emit(false, "invalid_arguments", argumentError, nil)
	}
	if operation == "apply" && (!applyPlatformSupported() || !releases.PublisherTrust().Configured()) {
		return emit(false, "apply_unavailable", "当前平台或构建尚未具备可信自动更新条件，请按发行说明手动更新", nil)
	}
	if (operation == "apply" || operation == "recover") && !options.confirmed {
		return emit(false, "confirmation_required", "请检查候选或操作编号，并使用 --confirm 明确确认", nil)
	}
	if ((operation == "download" || operation == "apply") && options.candidateID == "") || ((operation != "download" && operation != "apply") && options.candidateID != "") || ((operation == "recover") != (options.operationID != "")) || (options.confirmed && operation != "apply" && operation != "recover") {
		return emit(false, "invalid_arguments", "更新参数或编号无效", nil)
	}
	layout, err := productlayout.Resolve()
	if err != nil {
		return emit(false, "invalid_target", "无法确认当前程序布局。", nil)
	}
	rpm := runtime.GOOS == "linux" && productlayout.RPMManaged(layout)
	if rpm {
		if operation != "check" {
			return emit(false, "system_package_managed", "此安装由 RPM 管理，请使用系统包管理器更新。", nil)
		}
		if options.kind != "" {
			return emit(false, "invalid_arguments", "RPM 的更新包类型由当前安装确定。", nil)
		}
		options.kind = "gui-rpm"
		if productlayout.RPMCore(layout) {
			options.kind = "core-rpm"
		}
	} else {
		if options.kind == "" {
			options.kind = defaultPackageKind(runtime.GOOS, runtime.GOARCH, layout)
		}
		if options.kind != "setup" && options.kind != "gui-portable" && options.kind != "core-portable" {
			return emit(false, "invalid_arguments", "更新包类型无效。", nil)
		}
	}
	selection := releases.Selection{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: options.kind, Channel: options.channel, CurrentVersion: version}
	cacheBase, err := operations.CacheRoot()
	if err != nil {
		return emit(false, "cache_unavailable", "更新缓存不可用。", nil)
	}
	identity := sha256.Sum256([]byte(layout.ExecutableDirectory + "\x00" + options.kind + "\x00" + options.channel))
	root := filepath.Join(cacheBase, hex.EncodeToString(identity[:16]))
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{})
	if err != nil {
		return emit(false, "cache_unavailable", "更新缓存保护不可用。", nil)
	}
	statePath := filepath.Join(root, "state.json")
	// SecureStore creates/protects the cache directory even for a missing file.
	data, exists, err := store.Read(ctx, statePath, 2<<20)
	if err != nil {
		return emit(false, "cache_unavailable", "无法安全读取更新缓存。", nil)
	}
	var previous cached
	if exists {
		if err = jsonfile.DecodeStrict(data, &previous); err != nil || previous.SchemaVersion != 1 {
			return emit(false, "cache_invalid", "更新缓存无效。", nil)
		}
	}
	if operation == "status" {
		previous = projectInstalledVersion(previous, version)
		operationRoot, err := operations.DefaultRoot()
		if err != nil {
			return emit(false, "operation_status_unavailable", "更新执行记录无法确认，请检查更新状态后再操作。", nil)
		}
		execution, err := executionStatus(ctx, operationRoot, layout.ExecutableDirectory)
		if err != nil {
			return emit(false, "operation_status_unavailable", "更新执行记录无法确认，请检查更新状态后再操作。", nil)
		}
		if execution != nil {
			return emit(true, "ok", "", composeStatus(previous, exists, execution))
		}
		if !exists {
			return emit(true, "not_checked", "尚未检查更新。", nil)
		}
		return emit(true, "ok", "", previous)
	}
	backoffPath := filepath.Join(cacheBase, "github-backoff.json")
	if _, _, e := store.Read(ctx, backoffPath, 1024); e != nil {
		return emit(false, "cache_unavailable", "更新来源缓存不可用。", nil)
	}
	releaseLock, err := lockCache(filepath.Join(cacheBase, "github-operation.lock"))
	if err != nil {
		return emit(false, "update_busy", "另一项更新操作正在进行。", nil)
	}
	defer releaseLock()
	// The initial read protects the directory and serves read-only status. It
	// cannot authorize a mutation: another host may have completed a download
	// before this host acquired the lock. Refresh the authoritative snapshot.
	data, exists, err = store.Read(ctx, statePath, 2<<20)
	if err != nil {
		return emit(false, "cache_unavailable", "无法安全读取更新缓存。", nil)
	}
	previous = cached{}
	if exists {
		if err = jsonfile.DecodeStrict(data, &previous); err != nil || previous.SchemaVersion != 1 {
			return emit(false, "cache_invalid", "更新缓存无效。", nil)
		}
	}
	if b, exists, e := store.Read(ctx, backoffPath, 1024); e != nil {
		return emit(false, "cache_unavailable", "更新限流记录不可用。", nil)
	} else if exists {
		var wait backoff
		if jsonfile.DecodeStrict(b, &wait) != nil || wait.SchemaVersion != 1 {
			return emit(false, "cache_invalid", "更新限流记录无效。", nil)
		}
		if time.Now().Before(wait.Until) {
			return emit(false, "rate_limited", "GitHub 请求受限，请稍后重试。", wait)
		}
	}
	// All network stages share the same durable backoff, including installed
	// proof, repeated candidate authentication and recovery.
	failSource := func(err error, result any, applying bool) int {
		var limited *github.RateLimitError
		if errors.As(err, &limited) {
			b, _ := json.Marshal(backoff{SchemaVersion: 1, Until: limited.RetryAt})
			if e := store.Replace(context.WithoutCancel(ctx), backoffPath, b); e != nil {
				return emit(false, "cache_write_failed", "限流记录无法保存，请稍后重试", result)
			}
		}
		if applying {
			return emit(false, applyErrorCode(err), applyErrorMessage(err), result)
		}
		return emit(false, safeCode(err), safeMessage(err), result)
	}
	source := github.NewGitHub(nil)
	if operation == "recover" {
		result, err := recoverOperation(ctx, layout, version, buildID, options.operationID, source)
		if err != nil {
			return failSource(err, result, true)
		}
		return emit(true, "ok", "中断协调已结束，未重复启动或恢复连接", result)
	}
	result, err := source.Check(ctx, selection)
	if err != nil {
		return failSource(err, nil, false)
	}
	if rpm {
		if result.Candidate != nil {
			result.Candidate.ApplyReason = "system_package_managed"
		}
		return emit(true, "ok", "", cached{SchemaVersion: 1, Selection: selection, CheckedAt: time.Now().UTC(), Check: result})
	}
	projectApplyOffer(result, validApplyTarget(layout, options.kind), applyPlatformSupported())
	state := cached{SchemaVersion: 1, Selection: selection, CheckedAt: time.Now().UTC(), Check: result}
	if previous.Downloaded != "" {
		valid, err := inspectCached(root, previous.Downloaded, previous.Check.Candidate)
		if err != nil {
			return emit(false, "cache_invalid", "已下载缓存已变化，请先检查并修复缓存。", nil)
		}
		if valid && samePackage(previous.Check.Candidate, result.Candidate) {
			state.Downloaded = previous.Downloaded
		}
	}
	if (operation == "download" || operation == "apply") && (result.Candidate == nil || result.Candidate.ID != options.candidateID) {
		return emit(false, "candidate_changed", "候选版本已变化，请重新检查。", nil)
	}

	if operation == "apply" {
		if !validApplyTarget(layout, options.kind) {
			return emit(false, "invalid_target", "仅支持保持现有包型的受支持安装目标", nil)
		}
		if result.State != "available" || result.Candidate == nil {
			return emit(false, "not_newer", "候选版本不高于当前版本", nil)
		}
		if previous.Downloaded == "" || !samePackage(previous.Check.Candidate, result.Candidate) {
			return emit(false, "download_required", "请先下载当前准确候选，再确认更新", nil)
		}
		outcome, err := applyDownloaded(ctx, root, layout, version, buildID, source, *result.Candidate, previous.Downloaded, releaseRuntime)
		if err != nil {
			return failSource(err, outcome, true)
		}
		if outcome != nil && outcome.HandedOver {
			return emit(true, "handed_over", "安装器已接管，请在安装器中继续，完成后用 status 查询结果", outcome)
		}
		return emit(true, "ok", "更新已安装，恢复结果见返回状态", outcome)
	}
	// Keep one verified package per target/type/channel. Clear the old owned
	// leaf before obtaining a replacement; failed downloads can be retried.
	if previous.Downloaded != "" && previous.Downloaded != state.Downloaded {
		if err = removeCached(root, previous.Downloaded, previous.Check.Candidate); err != nil {
			return emit(false, "cache_cleanup_failed", "旧更新缓存无法清理；不会继续下载或安装。", nil)
		}
	}
	newDownload := ""
	if operation == "download" {
		if state.Downloaded == "" {
			path, err := github.Download(ctx, nil, *result.Candidate, root)
			if err != nil {
				return failSource(err, nil, false)
			}
			state.Downloaded = path
			newDownload = path
		}
	}
	b, err := json.Marshal(state)
	if err != nil {
		return emit(false, "update_failed", "更新操作失败。", nil)
	}
	if err = store.Replace(ctx, statePath, b); err != nil {
		if newDownload != "" {
			if cleanupErr := removeCached(root, newDownload, result.Candidate); cleanupErr != nil {
				return emit(false, "cache_cleanup_failed", "更新结果无法保存且缓存无法清理，请检查缓存。", nil)
			}
		}
		return emit(false, "cache_write_failed", "更新结果无法保存；不会启动安装。", nil)
	}
	return emit(true, "ok", "", state)
}
