// Package launchcontract owns the small, validated contract passed from a
// local client to its sidraviad child through the environment.
package launchcontract

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	EnvMode            = "SIDRAVIA_DAEMON_MODE"
	EnvDesktopOwnerPID = "SIDRAVIA_DESKTOP_OWNER_PID"
	EnvLogLevel        = "SIDRAVIA_LOG_LEVEL"
)

type Mode string

const (
	ModeHeadless Mode = "headless"
	ModeDesktop  Mode = "desktop"
)

var ErrInvalidOptions = errors.New("invalid daemon launch options")

// Options is immutable launch input. DesktopOwnerPID is present only for a
// desktop daemon and is deliberately not a command-line argument.
type Options struct {
	Mode            Mode
	DesktopOwnerPID int
}

func Headless() Options { return Options{Mode: ModeHeadless} }

func Desktop(ownerPID int) (Options, error) {
	return New(ModeDesktop, ownerPID)
}

func New(mode Mode, ownerPID int) (Options, error) {
	if mode == "" {
		mode = ModeHeadless
	}
	switch mode {
	case ModeHeadless:
		if ownerPID != 0 {
			return Options{}, ErrInvalidOptions
		}
	case ModeDesktop:
		if ownerPID <= 0 {
			return Options{}, ErrInvalidOptions
		}
	default:
		return Options{}, ErrInvalidOptions
	}
	return Options{Mode: mode, DesktopOwnerPID: ownerPID}, nil
}

func (o Options) Validate() error {
	_, err := New(o.Mode, o.DesktopOwnerPID)
	return err
}

func Parse(modeValue, ownerPIDValue string) (Options, error) {
	mode := Mode(strings.TrimSpace(modeValue))
	ownerPID := 0
	if ownerPIDValue != "" {
		parsed, err := strconv.Atoi(ownerPIDValue)
		if err != nil {
			return Options{}, ErrInvalidOptions
		}
		ownerPID = parsed
	}
	return New(mode, ownerPID)
}

// ChildEnvironment returns a new exact child environment. It removes all
// case-insensitive copies of the launch and log keys without mutating parent.
func ChildEnvironment(parent []string, options Options, logLevel string) ([]string, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if logLevel == "" {
		logLevel = "info"
	}
	result := make([]string, 0, len(parent)+3)
	for _, entry := range parent {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, EnvMode) || strings.EqualFold(name, EnvDesktopOwnerPID) || strings.EqualFold(name, EnvLogLevel) {
			continue
		}
		result = append(result, entry)
	}
	result = append(result, EnvMode+"="+string(options.Mode), EnvLogLevel+"="+logLevel)
	if options.Mode == ModeDesktop {
		result = append(result, fmt.Sprintf("%s=%d", EnvDesktopOwnerPID, options.DesktopOwnerPID))
	}
	return result, nil
}
