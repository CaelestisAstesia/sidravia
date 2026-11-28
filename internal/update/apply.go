package update

import (
	"context"
	"errors"
	"net/url"

	"sidravia/internal/release"
	"sidravia/internal/release/qtrepository"
	"sidravia/internal/update/internal/candidatecache"
	"sidravia/internal/update/internal/github"
	"sidravia/internal/update/internal/request"
)

var errSourceProof = errors.New("installed source publisher proof unavailable or mismatched")

var errOperation = errors.New("update operation unavailable or busy")
var errPreparationRejected = errors.New("desktop rejected update preparation")

type applyResult struct {
	HandedOver          bool   `json:"handedOver,omitempty"`
	FilesApplied        bool   `json:"filesApplied"`
	ApplicationRecovery string `json:"applicationRecovery"`
	Authentication      string `json:"authentication"`
}

func applyErrorCode(err error) string {
	var remote *url.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, github.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, errSourceProof):
		return "source_proof_unavailable"
	case errors.Is(err, qtrepository.ErrMaintenanceImage):
		return "maintenance_image_requires_reinstall"
	case errors.Is(err, errPreparationRejected):
		return "preparation_rejected"
	case errors.Is(err, errOperation):
		return "update_busy"
	}
	if code := request.SourceCode(err); code != "source_unavailable" {
		return code
	}
	if errors.As(err, &remote) {
		return "source_unavailable"
	}
	return platformApplyErrorCode(err)
}
func applyErrorMessage(err error) string {
	switch applyErrorCode(err) {
	case "apply_failed":
		return "更新未完成，请查看 status 确认执行阶段"
	case "source_proof_unavailable":
		return "当前安装缺少匹配的发行签名证明，请手动升级到可信基线"
	case "maintenance_image_requires_reinstall":
		return "此版本需要更换维护工具，请从发行页下载完整安装包并按说明升级。"
	case "update_busy":
		return "另一项更新仍在进行，或操作阶段不允许重复执行"
	case "preparation_rejected":
		return "程序未确认退出准备，请先保存或取消未完成的编辑后重试"
	case "cancelled":
		return "更新操作已取消，执行结果请查看 status"
	case "timeout":
		return "更新操作超时，执行结果请查看 status"
	}
	return request.SourceMessage(err)
}

func projectInstalledVersion(state candidatecache.State, version string) candidatecache.State {
	state.Selection.CurrentVersion = version
	if state.Check.Candidate != nil {
		c := *state.Check.Candidate
		state.Check.Candidate = &c
		current, e := release.ParseVersion(version)
		candidate, e2 := release.ParseVersion(c.Version)
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
