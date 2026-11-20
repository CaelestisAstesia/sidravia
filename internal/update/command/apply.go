package command

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"runtime"

	"sidravia/internal/productlayout"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/packagefiles"
	"sidravia/internal/update/releases"
)

var errSourceProof = errors.New("installed source publisher proof unavailable or mismatched")
var errManualRepair = errors.New("partial installation requires manual repair")
var errStagingCleanup = errors.New("update staging cleanup failed")
var errOperation = errors.New("update operation unavailable or busy")

type applyResult struct {
	Operation           *operationView `json:"operation,omitempty"`
	HandedOver          bool           `json:"handedOver,omitempty"`
	FilesApplied        bool           `json:"filesApplied"`
	ApplicationRecovery string         `json:"applicationRecovery"`
	Authentication      string         `json:"authentication"`
}

func operationResult(ctx context.Context, store *operations.Store) *applyResult {
	view, err := readExecutionStatus(ctx, store)
	if err != nil || view == nil {
		return nil
	}
	applied := view.Phase == operations.Applied || view.Phase == operations.Resuming || view.Phase == operations.ServiceStarting || view.Phase == operations.Completed || view.Phase == operations.ResumeFailed
	recovery := "not_started"
	record, _, err := store.Read(ctx)
	if view.Phase == operations.Completed {
		recovery = "ready"
		if err == nil && record.Recovery != nil && record.Recovery.Intent.Mode == "none" {
			recovery = "not_requested"
		}
	} else if view.Phase == operations.ResumeFailed {
		recovery = "not_confirmed"
	} else if view.Phase == operations.RolledBack {
		recovery = "source_files_restored"
	}
	return &applyResult{Operation: view, FilesApplied: applied, ApplicationRecovery: recovery, Authentication: "not_evaluated"}
}
func readIndex(target string) ([]byte, error) {
	root, err := os.OpenRoot(target)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(packagefiles.Name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > packagefiles.SizeLimit {
		return nil, errSourceProof
	}
	file, err := root.Open(packagefiles.Name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, current) {
		return nil, errSourceProof
	}
	data, err := io.ReadAll(io.LimitReader(file, packagefiles.SizeLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > packagefiles.SizeLimit {
		return nil, errSourceProof
	}
	return data, nil
}
func installedProof(ctx context.Context, source *releases.GitHub, layout productlayout.Layout, version, buildID string) (releases.VerifiedRelease, releases.Candidate, []byte, error) {
	proof, err := source.AuthenticateVersion(ctx, version)
	if err != nil {
		if errors.Is(err, releases.ErrSignatureMissing) || errors.Is(err, releases.ErrUntrusted) || errors.Is(err, releases.ErrMetadata) {
			err = errors.Join(errSourceProof, err)
		}
		return releases.VerifiedRelease{}, releases.Candidate{}, nil, err
	}
	if proof.BuildID() != buildID {
		return releases.VerifiedRelease{}, releases.Candidate{}, nil, errSourceProof
	}
	asset, err := proof.InstalledAsset(runtime.GOOS, runtime.GOARCH, defaultPackageKind(runtime.GOOS, runtime.GOARCH, layout))
	if err != nil {
		return releases.VerifiedRelease{}, releases.Candidate{}, nil, errors.Join(errSourceProof, err)
	}
	data, err := readIndex(layout.ExecutableDirectory)
	if err != nil {
		return releases.VerifiedRelease{}, releases.Candidate{}, nil, errors.Join(errSourceProof, err)
	}
	if _, err := proof.VerifyFiles(asset, data); err != nil {
		return releases.VerifiedRelease{}, releases.Candidate{}, nil, errors.Join(errSourceProof, err)
	}
	return proof, asset, data, nil
}
func applyErrorCode(err error) string {
	if code := platformApplyErrorCode(err); code == "handoff_unconfirmed" {
		return code
	}
	var remote *url.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, releases.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, errSourceProof):
		return "source_proof_unavailable"
	case errors.Is(err, errManualRepair):
		return "manual_repair_required"
	case errors.Is(err, operations.ErrConflict), errors.Is(err, errOperation):
		return "update_busy"
	}
	if code := safeCode(err); code != "source_unavailable" {
		return code
	}
	if errors.As(err, &remote) {
		return "source_unavailable"
	}
	if errors.Is(err, errStagingCleanup) {
		return "staging_cleanup_failed"
	}
	return platformApplyErrorCode(err)
}
func applyErrorMessage(err error) string {
	switch applyErrorCode(err) {
	case "handoff_unconfirmed":
		return "更新交接结果未能确认，请查看 status，勿重复应用更新"
	case "handoff_unavailable":
		return "无法从当前进程环境交接更新程序，请检查 status 或按发行说明手动更新"
	case "apply_failed":
		return "更新未完成，请查看 status 确认执行阶段"
	case "staging_cleanup_failed":
		return "更新暂存文件未能清理，安装结果请查看 status"
	case "source_proof_unavailable":
		return "当前安装缺少匹配的发行签名证明，请手动升级到可信基线"
	case "manual_repair_required":
		return "更新曾中断写入，请按发行说明手动修复，程序仍保持启动保护"
	case "update_busy":
		return "另一项更新仍在进行，或操作阶段不允许重复执行"
	case "update_rolled_back":
		return "更新失败，已恢复旧版程序文件。请用 daemon status 确认运行版本和状态"
	case "resume_failed":
		return "文件已安装，但程序恢复未确认，请检查运行状态"
	case "preparation_rejected":
		return "程序未确认退出准备，请先保存或取消未完成的编辑后重试"
	case "target_changed":
		return "程序文件、目标目录或已下载内容已变化，未继续更新"
	case "cancelled":
		return "更新操作已取消，执行结果请查看 status"
	case "timeout":
		return "更新操作超时，执行结果请查看 status"
	}
	return safeMessage(err)
}
func projectInstalledVersion(state cached, version string) cached {
	state.Selection.CurrentVersion = version
	if state.Check.Candidate != nil {
		c := *state.Check.Candidate
		state.Check.Candidate = &c
		current, e := releases.ParseVersion(version)
		candidate, e2 := releases.ParseVersion(c.Version)
		if e == nil && e2 == nil && candidate.Compare(current) <= 0 {
			c.CanApply = false
			c.ApplyReason = "not_newer"
			state.Check.State = "up_to_date"
			if candidate.Compare(current) < 0 {
				state.Check.State = "ahead_of_channel"
			}
			state.Check.Candidate = &c
		}
	}
	return state
}

// ReadOnly is shared with process entry points so status remains queryable
// during maintenance. Invalid arguments still reach the ordinary parser.
func ReadOnly(args []string) bool {
	return len(args) == 0 || args[0] == "status" || args[0] == "version" || args[0] == "help" || args[0] == "--help"
}
func ControlEntry(args []string) bool { return ReadOnly(args) || len(args) > 0 && args[0] == "recover" }
