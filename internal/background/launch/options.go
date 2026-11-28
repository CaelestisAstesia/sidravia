// Package launch owns the small, validated contract passed from a
// local client to its sidraviad child through the environment.
package launch

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	productlayout "sidravia/internal/installation/layout"
)

// ExitSingletonContention is reserved for an observed existing daemon host.
const ExitSingletonContention = 2

const (
	EnvMode                  = "SIDRAVIA_DAEMON_MODE"
	EnvDesktopOwnerStart     = "SIDRAVIA_DESKTOP_OWNER_START"
	EnvDesktopOwnerPID       = "SIDRAVIA_DESKTOP_OWNER_PID"
	EnvManagedLog            = "SIDRAVIA_MANAGED_LOG"
	EnvLogLevel              = "SIDRAVIA_LOG_LEVEL"
	EnvSkipAutoLogin         = "SIDRAVIA_SKIP_AUTO_LOGIN"
	EnvNamespace             = "SIDRAVIA_NAMESPACE"
	EnvRecoveryOperation     = "SIDRAVIA_UPDATE_OPERATION"
	EnvRecoveryToken         = "SIDRAVIA_UPDATE_TOKEN"
	EnvRecoveryConfiguration = "SIDRAVIA_UPDATE_CONFIGURATION"
)

// EnvSystemdPackage pins the data layout selected when registering a user unit.
const EnvSystemdPackage = "SIDRAVIA_SYSTEMD_PACKAGE"

type Mode string

const (
	ModeHeadless Mode = "headless"
	ModeDesktop  Mode = "desktop"
	ModeSystemd  Mode = "systemd"
)

var ErrInvalidOptions = errors.New("invalid daemon launch options")

// Options is immutable launch input. DesktopOwnerPID is present only for a
// desktop daemon and is deliberately not a command-line argument.
type Options struct {
	Mode                Mode
	DesktopOwnerPID     int
	DesktopOwnerStart   string
	Namespace           string
	SkipAutoLogin       bool
	SwitchOperation     string
	SwitchConfiguration string
	LogLevel            string
}

func Headless() Options { return Options{Mode: ModeHeadless} }

func Desktop(ownerPID int) (Options, error) {
	return New(ModeDesktop, ownerPID, "")
}

func New(mode Mode, ownerPID int, namespace string) (Options, error) {
	if mode == "" {
		mode = ModeHeadless
	}
	ns, err := productlayout.NewNamespace(namespace)
	if err != nil {
		return Options{}, ErrInvalidOptions
	}
	switch mode {
	case ModeHeadless:
		if ownerPID != 0 {
			return Options{}, ErrInvalidOptions
		}
	case ModeSystemd:
		if ownerPID != 0 || !ns.IsProduction() || runtime.GOOS != "linux" {
			return Options{}, ErrInvalidOptions
		}
	case ModeDesktop:
		if ownerPID <= 0 {
			return Options{}, ErrInvalidOptions
		}
	default:
		return Options{}, ErrInvalidOptions
	}
	return Options{Mode: mode, DesktopOwnerPID: ownerPID, Namespace: ns.String()}, nil
}

func (o Options) Validate() error {
	if o.DesktopOwnerStart != "" {
		n, err := strconv.ParseUint(o.DesktopOwnerStart, 10, 64)
		if o.Mode != ModeDesktop || err != nil || n == 0 {
			return ErrInvalidOptions
		}
	}
	_, err := New(o.Mode, o.DesktopOwnerPID, o.Namespace)
	return err
}

func Parse(modeValue, ownerPIDValue, namespaceValue string) (Options, error) {
	mode := Mode(strings.TrimSpace(modeValue))
	ownerPID := 0
	if ownerPIDValue != "" {
		parsed, err := strconv.Atoi(ownerPIDValue)
		if err != nil {
			return Options{}, ErrInvalidOptions
		}
		ownerPID = parsed
	}
	return New(mode, ownerPID, namespaceValue)
}

// ChildEnvironment returns a new exact child environment. It removes all
// case-insensitive copies of the launch and log keys without mutating parent.
func ChildEnvironment(parent []string, options Options, logLevel string) ([]string, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if logLevel == "" {
		logLevel = "debug"
	}
	result := make([]string, 0, len(parent)+4)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, EnvDesktopOwnerStart) && (options.Mode != ModeDesktop || options.DesktopOwnerStart != "") || strings.EqualFold(name, EnvManagedLog) || strings.EqualFold(name, EnvMode) || strings.EqualFold(name, EnvDesktopOwnerPID) || strings.EqualFold(name, EnvLogLevel) || strings.EqualFold(name, EnvNamespace) || strings.EqualFold(name, EnvSkipAutoLogin) || strings.EqualFold(name, EnvRecoveryOperation) || strings.EqualFold(name, EnvRecoveryToken) || strings.EqualFold(name, EnvRecoveryConfiguration) {
			continue
		}
		result = append(result, entry)
	}
	result = append(result, EnvMode+"="+string(options.Mode), EnvLogLevel+"="+logLevel, EnvManagedLog+"=1")
	if options.SkipAutoLogin {
		result = append(result, EnvSkipAutoLogin+"=1")
	}
	if options.Namespace != "" {
		result = append(result, EnvNamespace+"="+options.Namespace)
	}
	if options.Mode == ModeDesktop {
		result = append(result, fmt.Sprintf("%s=%d", EnvDesktopOwnerPID, options.DesktopOwnerPID))
		if options.DesktopOwnerStart != "" {
			result = append(result, EnvDesktopOwnerStart+"="+options.DesktopOwnerStart)
		}
	}
	return result, nil
}
