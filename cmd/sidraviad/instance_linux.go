//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"sidravia/internal/installation/userservice"
	"sidravia/internal/installation/userservice/registration"
	"sidravia/internal/process/linuxproc"
	"strconv"
	"time"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/background/userlease"
	"sidravia/internal/daemon/host"
)

// acquireUserInstance holds the cross-installation lease until the entire
// daemon exits. The account home from the user database stays the same across
// logins and XDG overrides. The lease is removed on clean shutdown.
func acquireUserInstance() (func() error, error) {
	options, err := launchcontract.Parse(os.Getenv(launchcontract.EnvMode),
		os.Getenv(launchcontract.EnvDesktopOwnerPID), os.Getenv(launchcontract.EnvNamespace))
	if err != nil {
		return nil, err
	}
	noop := func() error { return nil }
	if options.Namespace != "" {
		return noop, nil
	}
	release, err := userlease.Acquire()
	if errors.Is(err, userlease.ErrBusy) {
		return nil, host.ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = release()
		}
	}()
	registered, exists, err := registration.Lookup()
	if err != nil {
		return nil, err
	}
	if options.Mode == launchcontract.ModeSystemd {
		exe, err := os.Executable()
		if err != nil || !exists || filepath.Dir(exe) != registered.ExecutableDirectory ||
			os.Getenv(launchcontract.EnvSystemdPackage) != string(registered.Mode) {
			return nil, errors.New("daemon does not match user service registration")
		}
	} else if exists {
		// Systemd is the sole launch path while a registration owns the user.
		return nil, host.ErrAlreadyRunning
	}
	accepted = true
	return release, nil
}

func configureServiceSwitch(options *launchcontract.Options, paths defaultPaths) error {
	if options.Namespace != "" {
		return nil
	}
	state, pending, err := userservice.PendingServiceSwitch()
	if err != nil || !pending {
		return err
	}
	if state.Mode == "replacement" {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Dir(executable) != state.Layout.ExecutableDirectory ||
		paths.configurations != state.Layout.ConfigurationsPath || paths.runtimeInfo != state.Layout.RuntimeInfoPath ||
		options.RecoveryOperation != "" {
		return userservice.ErrServiceRecovery
	}
	if options.Mode == launchcontract.ModeDesktop {
		ownerStart := uint64(0)
		switch options.DesktopOwnerPID {
		case state.OwnerPID:
			ownerStart = state.OwnerStart
		case state.TargetOwnerPID:
			ownerStart = state.TargetOwnerStart
		}
		if ownerStart == 0 {
			return userservice.ErrServiceRecovery
		}
		alive, err := linuxproc.GenerationRunning(options.DesktopOwnerPID, strconv.FormatUint(ownerStart, 10))
		if err != nil || !alive {
			return errors.Join(userservice.ErrServiceRecovery, err)
		}
	} else if options.Mode == launchcontract.ModeHeadless &&
		(state.Mode == string(launchcontract.ModeDesktop) ||
			state.Mode == string(launchcontract.ModeSystemd) && state.TargetOwnerPID != 0) {
		return userservice.ErrServiceRecovery
	}
	// On every generation during the handoff, including supervisor retries,
	// the saved intent wins over the normal boot policy.
	options.SkipAutoLogin = true
	options.SwitchOperation = state.ID
	if time.Now().Before(state.Expires) {
		options.SwitchConfiguration = state.ConfigurationID
	}
	return nil
}
