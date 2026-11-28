package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"sidravia/internal/document/jsondoc"
	"sidravia/internal/document/securefile"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/candidatecache"
)

const qtOutcomeFilename = "qt-outcome.json"

const qtLogFilename = "qt-maintenance.log"

func openQtLog(cacheRoot string) (*os.File, error) {
	cache, err := os.OpenRoot(cacheRoot)
	if err != nil {
		return nil, err
	}
	defer cache.Close()
	if err := cache.Remove(qtLogFilename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return cache.OpenFile(qtLogFilename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

type qtStatusResult struct {
	candidatecache.State
	Installation qtOutcome `json:"installation"`
}

type qtOutcome struct {
	TargetVersion string `json:"targetVersion"`
	State         string `json:"state"`
}

func writeQtOutcome(ctx context.Context, path, version, state string) error {
	store, err := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if err != nil {
		return err
	}
	data, err := json.Marshal(qtOutcome{TargetVersion: version, State: state})
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
		return "更新结果尚未确认，请重新打开程序核对版本。如未更新，可重试或用可信离线安装包修复"
	}
}
