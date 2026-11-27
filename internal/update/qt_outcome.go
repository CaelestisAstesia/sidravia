package update

import (
	"context"
	"encoding/json"
	"path/filepath"

	"sidravia/internal/document/jsondoc"
	"sidravia/internal/document/securefile"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/candidatecache"
)

const qtOutcomeFilename = "qt-outcome.json"

type qtStatusResult struct {
	candidatecache.State
	Installation qtOutcome `json:"installation"`
}

type qtOutcome struct {
	TargetVersion string `json:"targetVersion"`
	State         string `json:"state"`
}

func writeQtPending(ctx context.Context, path, version string) error {
	store, err := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if err != nil {
		return err
	}
	data, err := json.Marshal(qtOutcome{TargetVersion: version, State: "pending"})
	if err != nil {
		return err
	}
	return store.Replace(ctx, path, data)
}

func readQtOutcome(ctx context.Context, cacheRoot, currentVersion string) (qtOutcome, bool, error) {
	store, err := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if err != nil {
		return qtOutcome{}, false, err
	}
	data, exists, err := store.Read(ctx, filepath.Join(cacheRoot, qtOutcomeFilename), 160)
	if err != nil || !exists {
		return qtOutcome{}, false, err
	}
	var result qtOutcome
	if err := jsondoc.DecodeStrict(data, &result); err != nil {
		return qtOutcome{}, false, release.ErrMetadata
	}
	version, err := release.ParseVersion(result.TargetVersion)
	if err != nil {
		return qtOutcome{}, false, release.ErrMetadata
	}
	switch result.State {
	case "completed":
		current, err := release.ParseVersion(currentVersion)
		if err != nil || current.Compare(version) < 0 {
			result.State = "version_unconfirmed"
		}
	case "pending", "failed":
	default:
		return qtOutcome{}, false, release.ErrMetadata
	}
	return result, true, nil
}

func (outcome qtOutcome) message() string {
	switch outcome.State {
	case "completed":
		return "维护工具已报告完成，当前程序版本已确认"
	case "failed":
		return "维护工具报告更新失败，请重新检查并重试，或使用可信离线安装包修复"
	default:
		return "更新结果尚未确认，请等待维护工具结束后重新打开程序检查版本"
	}
}
