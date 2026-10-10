package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
)

type guiLogDirectoryError struct {
	message string
	cause   error
}

func (e *guiLogDirectoryError) Error() string { return e.message }
func (e *guiLogDirectoryError) Unwrap() error { return e.cause }
func logDirectoryFailure(message string, cause error) error {
	return &guiLogDirectoryError{message, cause}
}

var errLogDirectoryUnsupported = errors.New("log directory platform unsupported")

type guiLogDirectoryDependencies struct {
	namespace func() (productlayout.Namespace, error)
	resolve   func(productlayout.Namespace) (productlayout.Layout, error)
	stat      func(string) (os.FileInfo, error)
	open      func(string) error
}

func openGUILogDirectory() error {
	if runtime.GOOS != "windows" {
		return logDirectoryFailure("当前平台不支持打开日志目录。", errLogDirectoryUnsupported)
	}
	return openGUILogDirectoryWith(guiLogDirectoryDependencies{
		namespace: func() (productlayout.Namespace, error) {
			return productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
		},
		resolve: productlayout.ResolveNamespace, stat: os.Stat, open: openLogDirectoryNative,
	})
}
func openGUILogDirectoryWith(deps guiLogDirectoryDependencies) error {
	ns, err := deps.namespace()
	if err != nil {
		return logDirectoryFailure("无法确定本实例日志目录。", err)
	}
	layout, err := deps.resolve(ns)
	if err != nil {
		return logDirectoryFailure("无法确定本实例日志目录。", err)
	}
	path := filepath.Dir(layout.DaemonLogPath)
	if !filepath.IsAbs(layout.DaemonLogPath) || !filepath.IsAbs(path) {
		return logDirectoryFailure("无法确定本实例日志目录。", errors.New("log path is not absolute"))
	}
	info, err := deps.stat(path)
	if err != nil {
		return logDirectoryFailure("日志目录不存在或无法访问。", err)
	}
	if !info.IsDir() {
		return logDirectoryFailure("日志目录不存在或无法访问。", errors.New("log directory is not a directory"))
	}
	if err := deps.open(path); err != nil {
		return logDirectoryFailure("打开日志目录失败，请重试。", err)
	}
	return nil
}
func newGUILogDirectoryCommand() *cobra.Command {
	return &cobra.Command{Use: "open-log-directory", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return wrapCommandOperation(openGUILogDirectory()) }}
}
