// Package command hosts the shared updater on desktop. Application credentials
// and Windows installer mechanics do not belong in this entry point.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/productlayout"
	"sidravia/internal/update/releases"
)

type response struct {
	SchemaVersion int    `json:"schemaVersion"`
	OK            bool   `json:"ok"`
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
	Result        any    `json:"result,omitempty"`
}
type backoff struct {
	SchemaVersion int       `json:"schemaVersion"`
	Until         time.Time `json:"until"`
}

type cached struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Selection     releases.Selection   `json:"selection"`
	CheckedAt     time.Time            `json:"checkedAt"`
	Check         releases.CheckResult `json:"check"`
	Downloaded    string               `json:"downloaded,omitempty"`
}

func Run(ctx context.Context, args []string, version, buildID string, out io.Writer) int {
	emit := func(ok bool, code, message string, result any) int {
		err := json.NewEncoder(out).Encode(response{1, ok, code, message, result})
		if err != nil {
			return 1
		}
		if !ok {
			return 1
		}
		return 0
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		return emit(true, "ok", "Sidravia updater: check | download --candidate ID | status | version", nil)
	}
	if args[0] == "version" {
		if len(args) != 1 {
			return emit(false, "invalid_arguments", "更新参数无效。", nil)
		}
		return emit(true, "ok", "", map[string]string{"productVersion": version, "buildId": buildID})
	}
	operation := args[0]
	if operation != "check" && operation != "download" && operation != "status" && operation != "apply" {
		return emit(false, "invalid_arguments", "未知更新操作。", nil)
	}
	flags := flag.NewFlagSet("updater", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	defaultChannel := "stable"
	if v, e := releases.ParseVersion(version); e == nil && v.Prerelease() {
		defaultChannel = "preview"
	}
	channel := flags.String("channel", defaultChannel, "")
	kind := flags.String("package", "", "")
	candidateID := flags.String("candidate", "", "")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return emit(false, "invalid_arguments", "更新参数无效。", nil)
	}
	if *channel != "stable" && *channel != "preview" {
		return emit(false, "invalid_arguments", "更新通道无效。", nil)
	}
	if operation == "apply" {
		return emit(false, "apply_unavailable", "当前构建尚未接入自动安装。请按发行说明手动更新。", nil)
	}
	if (operation == "check" || operation == "status") && *candidateID != "" || operation == "download" && *candidateID == "" {
		return emit(false, "invalid_arguments", "下载需要准确的候选编号。", nil)
	}
	layout, err := productlayout.Resolve()
	if err != nil {
		return emit(false, "invalid_target", "无法确认当前程序布局。", nil)
	}
	if *kind == "" {
		*kind = "core-portable"
		if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
			if layout.Mode == productlayout.ModeInstalled {
				*kind = "setup"
			} else if info, e := os.Stat(filepath.Join(layout.ExecutableDirectory, "Sidravia.exe")); e == nil && info.Mode().IsRegular() {
				*kind = "gui-portable"
			}
		}
	}
	if *kind != "setup" && *kind != "gui-portable" && *kind != "core-portable" {
		return emit(false, "invalid_arguments", "更新包类型无效。", nil)
	}
	selection := releases.Selection{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: *kind, Channel: *channel, CurrentVersion: version}
	root, err := os.UserCacheDir()
	if err != nil {
		return emit(false, "cache_unavailable", "更新缓存不可用。", nil)
	}
	identity := sha256.Sum256([]byte(layout.ExecutableDirectory + "\x00" + *kind + "\x00" + *channel))
	cacheBase := filepath.Join(root, "Sidravia", "updates")
	root = filepath.Join(cacheBase, hex.EncodeToString(identity[:16]))
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
	source := releases.NewGitHub(nil)
	result, err := source.Check(ctx, selection)
	if err != nil {
		var limited *releases.RateLimitError
		if errors.As(err, &limited) {
			b, _ := json.Marshal(backoff{SchemaVersion: 1, Until: limited.RetryAt})
			if e := store.Replace(ctx, backoffPath, b); e != nil {
				return emit(false, "cache_write_failed", "限流记录无法保存，请稍后重试。", nil)
			}
		}
		return emit(false, safeCode(err), safeMessage(err), nil)
	}
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
	if operation == "download" && (result.Candidate == nil || result.Candidate.ID != *candidateID) {
		return emit(false, "candidate_changed", "候选版本已变化，请重新检查。", nil)
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
			path, err := releases.Download(ctx, nil, *result.Candidate, root)
			if err != nil {
				return emit(false, safeCode(err), safeMessage(err), nil)
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
func safeCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, releases.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, releases.ErrIncomplete):
		return "incomplete_query"
	case errors.Is(err, releases.ErrIntegrity):
		return "integrity_failed"
	case errors.Is(err, releases.ErrMetadata):
		return "invalid_metadata"
	case errors.Is(err, releases.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, releases.ErrVersion):
		return "unknown_current_version"
	}
	return "source_unavailable"
}
func safeMessage(err error) string {
	switch safeCode(err) {
	case "cancelled":
		return "更新操作已取消。"
	case "timeout":
		return "更新操作超时。"
	case "rate_limited":
		return "GitHub 请求受限，请稍后重试。"
	case "incomplete_query":
		return "发行信息未完整获取，请稍后重试。"
	case "integrity_failed":
		return "下载文件校验失败，不能用于更新。"
	case "unsupported":
		return "当前平台尚无适用的更新能力。"
	case "unknown_current_version":
		return "当前构建版本无法用于更新比较。"
	case "invalid_metadata":
		return "发行信息无效，已停止操作。"
	}
	return "更新来源暂时不可用，请稍后重试。"
}
