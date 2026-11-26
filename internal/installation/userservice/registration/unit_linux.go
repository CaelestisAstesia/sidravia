//go:build linux

package registration

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
)

// UnitName is the one systemd unit name managed by Sidravia.
const UnitName = "sidravia.service"

// renderUserUnit builds the unit for one resolved package instance. It captures
// the installed XDG roots so a boot-time user manager can use the same data as
// the caller that registered it. Portable data stays with its package.
func renderUserUnit(layout productlayout.Layout) ([]byte, error) {
	if !layout.Namespace.IsProduction() {
		return nil, errors.New("user service requires the production namespace")
	}
	executable, err := quoteUnitPath(filepath.Join(layout.ExecutableDirectory, "sidraviad"))
	if err != nil {
		return nil, err
	}
	env := "Environment=" + launchcontract.EnvMode + "=" + string(launchcontract.ModeSystemd) + "\n" +
		"Environment=" + launchcontract.EnvSystemdPackage + "=" + string(layout.Mode) + "\n" +
		"Environment=" + launchcontract.EnvLogLevel + "=info\n"
	switch layout.Mode {
	case productlayout.ModeInstalled:
		for _, field := range []struct{ key, path string }{
			{"XDG_CONFIG_HOME", layout.ConfigurationsPath},
			{"XDG_CACHE_HOME", layout.RuntimeInfoPath},
		} {
			dir := filepath.Dir(field.path)
			if filepath.Base(dir) != "Sidravia" {
				return nil, errors.New("unexpected installed data directory")
			}
			value, err := quoteUnitPath(filepath.Dir(dir))
			if err != nil {
				return nil, err
			}
			env += "Environment=\"" + field.key + "=" + strings.TrimPrefix(value, "\"") + "\n"
		}
	case productlayout.ModePortable:
		if layout.ServiceCacheRoot != "" {
			value, err := quoteUnitPath(layout.ServiceCacheRoot)
			if err != nil {
				return nil, err
			}
			env += "Environment=\"XDG_CACHE_HOME=" + strings.TrimPrefix(value, "\"") + "\n"
		}
	default:
		return nil, errors.New("unknown package layout")
	}

	// The colon disables systemd variable substitution in the command.
	return []byte("# Managed by Sidravia\n" +
		"[Unit]\nDescription=Sidravia campus authentication\n" +
		"StartLimitIntervalSec=60s\nStartLimitBurst=3\n\n" +
		"[Service]\nType=exec\nExecStart=\":" + strings.TrimPrefix(executable, "\"") + "\n" +
		env +
		"UnsetEnvironment=" + strings.Join([]string{
		launchcontract.EnvNamespace, launchcontract.EnvManagedLog, launchcontract.EnvDesktopOwnerPID,
		launchcontract.EnvDesktopOwnerStart, launchcontract.EnvSkipAutoLogin,
		launchcontract.EnvRecoveryOperation, launchcontract.EnvRecoveryToken,
		launchcontract.EnvRecoveryConfiguration,
	}, " ") + "\n" +
		"Restart=on-failure\nRestartSec=2s\n" +
		"RestartPreventExitStatus=" + strconv.Itoa(launchcontract.ExitSingletonContention) + "\n" +
		"TimeoutStopSec=15s\n\n" +
		"[Install]\nWantedBy=default.target\n"), nil
}

func quoteUnitPath(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !utf8.ValidString(path) ||
		strings.ContainsFunc(path, unicode.IsControl) {
		return "", errors.New("invalid systemd path")
	}
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(path) + "\"", nil
}
