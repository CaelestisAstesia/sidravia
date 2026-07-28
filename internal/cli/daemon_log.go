package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const daemonLogRotateThreshold int64 = 10 * 1024 * 1024

func prepareDaemonLog(cacheRoot string) (*os.File, error) {
	logDir := filepath.Join(cacheRoot, "Sidravia", "logs")
	logPath := filepath.Join(logDir, "sidraviad.log")
	backupPath := logPath + ".1"

	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 sidraviad 日志目录: %w", err)
	}
	info, err := os.Stat(logPath)
	switch {
	case err == nil && info.Size() >= daemonLogRotateThreshold:
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("删除 sidraviad 旧日志备份: %w", err)
		}
		if err := os.Rename(logPath, backupPath); err != nil {
			return nil, fmt.Errorf("轮转 sidraviad 日志: %w", err)
		}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("检查 sidraviad 日志: %w", err)
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开 sidraviad 日志: %w", err)
	}
	return file, nil
}
