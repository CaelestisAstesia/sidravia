//go:build !windows

package cli

import "errors"

// runInstallCommand and runUninstallCommand are Windows-only integration
// operations. Other platforms return a clear Unsupported result and never touch
// PATH, registry or task state.
func runInstallCommand(logLevel string) error {
	return errors.New("仅为 Windows 提供安装集成（unsupported）")
}

func runUninstallCommand() error {
	return errors.New("仅为 Windows 提供卸载集成（unsupported）")
}
