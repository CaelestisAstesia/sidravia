package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const daemonLogRotateThreshold int64 = 10 * 1024 * 1024

func prepareDaemonLog(logPath string) (*os.File, error) {
	protection, err := newDaemonLogProtection()
	if err != nil {
		return nil, fmt.Errorf("准备 sidraviad 日志保护: %w", err)
	}
	return prepareDaemonLogWith(logPath, defaultDaemonLogOperations(protection))
}

type daemonLogProtection interface {
	protectDirectory(string) error
	protectFile(string) error
}

type daemonLogOperations struct {
	mkdirAll   func(string, os.FileMode) error
	stat       func(string) (os.FileInfo, error)
	remove     func(string) error
	rename     func(string, string) error
	openFile   func(string, int, os.FileMode) (*os.File, error)
	protection daemonLogProtection
}

func defaultDaemonLogOperations(protection daemonLogProtection) daemonLogOperations {
	return daemonLogOperations{
		mkdirAll: os.MkdirAll, stat: os.Stat, remove: os.Remove, rename: os.Rename,
		openFile: os.OpenFile, protection: protection,
	}
}

func prepareDaemonLogWith(logPath string, operations daemonLogOperations) (*os.File, error) {
	logDir := filepath.Dir(logPath)
	backupPath := logPath + ".1"

	if err := operations.mkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 sidraviad 日志目录: %w", err)
	}
	if err := operations.protection.protectDirectory(logDir); err != nil {
		return nil, fmt.Errorf("保护 sidraviad 日志目录: %w", err)
	}
	info, err := operations.stat(logPath)
	currentExists := err == nil
	switch {
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("检查 sidraviad 日志: %w", err)
	}
	_, backupErr := operations.stat(backupPath)
	backupExists := backupErr == nil
	if backupErr != nil && !errors.Is(backupErr, os.ErrNotExist) {
		return nil, fmt.Errorf("检查 sidraviad 日志备份: %w", backupErr)
	}
	if currentExists {
		if err := operations.protection.protectFile(logPath); err != nil {
			return nil, fmt.Errorf("保护 sidraviad 当前日志: %w", err)
		}
	}
	if backupExists {
		if err := operations.protection.protectFile(backupPath); err != nil {
			return nil, fmt.Errorf("保护 sidraviad 日志备份: %w", err)
		}
	}
	if currentExists && info.Size() >= daemonLogRotateThreshold {
		if err := operations.remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("删除 sidraviad 旧日志备份: %w", err)
		}
		if err := operations.rename(logPath, backupPath); err != nil {
			return nil, fmt.Errorf("轮转 sidraviad 日志: %w", err)
		}
	}
	file, err := operations.openFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开 sidraviad 日志: %w", err)
	}
	if err := operations.protection.protectFile(logPath); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("保护 sidraviad 新日志: %w", err)
	}
	return file, nil
}
