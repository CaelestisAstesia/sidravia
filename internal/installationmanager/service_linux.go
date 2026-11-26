//go:build linux

package installationmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"sidravia/internal/background/userlease"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/userservice/registration"
)

func managerCommand(ctx context.Context, binary string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "LC_ALL=C", "SYSTEMD_PAGER=cat", "SYSTEMD_COLORS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", filepath.Base(binary), args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

func control(ctx context.Context, args ...string) (string, error) {
	return managerCommand(ctx, "/usr/bin/systemctl", append([]string{"--user", "--no-ask-password"}, args...)...)
}

func serviceSwitchStatus(path string, state ServiceState) ServiceState {
	pending, exists, journalErr := loadServiceSwitch(switchPath(path))
	state.RecoveryPending = exists
	state.CanDiscardReplacement = exists && pending.Mode == "replacement"
	state.RecoveryBlocked = journalErr != nil
	state.CanDiscardDamaged = errors.Is(journalErr, ErrServiceJournalDamaged)
	return state
}

func readServiceState(ctx context.Context, path string, layout productlayout.Layout, owned bool) (ServiceState, error) {
	state := serviceSwitchStatus(path, ServiceState{Registered: owned, Directory: layout.ExecutableDirectory})
	out, err := control(ctx, "show", "--property=LoadState,ActiveState,UnitFileState,FragmentPath,DropInPaths", registration.UnitName)
	if err != nil {
		// With no systemd instance and with no unit or switch record on
		// disk, the application can still own its ordinary XDG startup.
		if !owned && !state.RecoveryBlocked && !state.RecoveryPending {
			if _, statErr := os.Stat("/run/systemd/system"); errors.Is(statErr, os.ErrNotExist) {
				state.RegistrationKnown = true
			}
		}
		return state, errors.Join(ErrServiceUnavailable, err)
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, "=")
		fields[key] = value
	}
	state.Supported = true
	if hasCustomDropIns(fields["DropInPaths"]) || fields["LoadState"] == "masked" ||
		(owned && (fields["LoadState"] != "loaded" || fields["FragmentPath"] != path)) ||
		(!owned && (fields["FragmentPath"] != "" || fields["LoadState"] != "not-found")) {
		return state, ErrServiceConflict
	}
	switch fields["ActiveState"] {
	case "active":
		state.Running = true
	case "inactive", "failed":
	default:
		return state, ErrServiceConflict
	}
	state.RegistrationKnown = true
	state.Enabled = fields["UnitFileState"] == "enabled"
	linger, err := managerCommand(ctx, "/usr/bin/loginctl", "show-user", strconv.Itoa(os.Geteuid()), "--property=Linger", "--value")
	if err != nil {
		return state, errors.Join(ErrServiceUnavailable, err)
	}
	state.Linger = linger == "yes"
	return state, nil
}

// Explicit starts can retry once when systemd rejected the job before
// spawning a process. Automatic restarts still use the unit's normal limit.
func startService(ctx context.Context) error {
	_, err := control(ctx, "start", registration.UnitName)
	if err == nil {
		return nil
	}
	result, inspectErr := control(ctx, "show", "--property=Result", "--value", registration.UnitName)
	if inspectErr != nil || result != "start-limit-hit" {
		return errors.Join(err, inspectErr)
	}
	if _, resetErr := control(ctx, "reset-failed", registration.UnitName); resetErr != nil {
		return errors.Join(err, resetErr)
	}
	_, err = control(ctx, "start", registration.UnitName)
	return err
}

func stopService(ctx context.Context, wasRunning bool) error {
	if _, err := control(ctx, "stop", registration.UnitName); err != nil {
		return err
	}
	if !wasRunning {
		return nil
	}
	out, err := control(ctx, "show", "--property=ActiveState,Result,ExecMainCode,ExecMainStatus", registration.UnitName)
	if err != nil {
		return errors.Join(ErrServiceStop, err)
	}
	result := "\n" + out + "\n"
	// systemd may collect a successfully stopped disabled unit before
	// the next command, yielding a fresh inactive unit with ExecMainCode=0.
	// Failed units are retained, so the Result check is still required.
	if !strings.Contains(result, "\nActiveState=inactive\n") ||
		!strings.Contains(result, "\nResult=success\n") ||
		(!strings.Contains(result, "\nExecMainCode=1\n") && !strings.Contains(result, "\nExecMainCode=0\n")) ||
		!strings.Contains(result, "\nExecMainStatus=0\n") {
		return ErrServiceStop
	}
	return nil
}

func hasCustomDropIns(paths string) bool {
	for _, path := range strings.Fields(paths) {
		if filepath.Dir(path) != "/usr/lib/systemd/user/service.d" {
			return true
		}
	}
	return false
}

func clearRegistrationScratch(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 ||
		!info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return ErrServiceConflict
	}
	return os.Remove(path)
}

func saveUnit(path string, contents []byte) error {
	staging := path + ".sidravia-tmp"
	if err := clearRegistrationScratch(staging); err != nil {
		return err
	}
	if contents == nil {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	file, err := os.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(staging)
	_, writeErr := file.Write(contents)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Rename(staging, path)
}

func userService(ctx context.Context, action string, layout productlayout.Layout, ready func(context.Context, productlayout.Layout) error) (state ServiceState, resultErr error) {
	path, err := registration.UnitPath()
	if err != nil || !layout.Namespace.IsProduction() {
		return state, errors.Join(ErrServiceUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	layout, err = serviceTarget(layout, action)
	if err != nil {
		return state, err
	}
	if action == "register" || action == "replace" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return state, err
		}
	}
	// The observer and login-manager actions keep their existing behavior
	// without requiring a writable registration directory.
	switch action {
	case "status", "linger-on", "linger-off":
		return applyService(ctx, path, action, layout, ready, false)
	}
	core, err := openServiceLease(path)
	if errors.Is(err, os.ErrNotExist) && action == "unregister" {
		return readServiceState(ctx, path, productlayout.Layout{}, false)
	}
	if err != nil {
		return state, err
	}
	defer func() { resultErr = errors.Join(resultErr, core.close()) }()
	err = core.run(ctx, func(path string) error {
		state, err = applyService(ctx, path, action, layout, ready, false)
		return err
	})
	return state, err
}

func serviceTarget(layout productlayout.Layout, action string) (productlayout.Layout, error) {
	if layout.Mode == productlayout.ModePortable && layout.ServiceCacheRoot == "" && (action == "register" || action == "replace") {
		cache, err := os.UserCacheDir()
		if err != nil || !filepath.IsAbs(cache) {
			return productlayout.Layout{}, errors.Join(ErrServiceUnavailable, err)
		}
		layout.ServiceCacheRoot = filepath.Clean(cache)
	}
	return layout, nil
}

func applyService(ctx context.Context, path, action string, layout productlayout.Layout, ready serviceReadiness, switching bool) (state ServiceState, resultErr error) {
	if !switching && action != "status" {
		if _, pending, err := loadServiceSwitch(switchPath(path)); err != nil {
			return state, errors.Join(ErrServiceJournal, err)
		} else if pending {
			return state, ErrServiceRecovery
		}
	}
	if action == "discard-damaged" {
		if !switching {
			return state, ErrServiceJournal
		}
		if err := discardDamagedServiceSwitch(switchPath(path)); err != nil {
			return serviceSwitchStatus(path, state), err
		}
		// Clearing a local record does not require an online manager or a
		// usable registration. Report unavailable observation separately.
		actual, err := applyService(ctx, path, "status", layout, nil, switching)
		if err != nil {
			actual.Supported = false
		}
		return actual, nil
	}
	oldLayout, oldData, err := registration.Read(path)
	if err != nil {
		return serviceSwitchStatus(path, state), err
	}
	before, err := readServiceState(ctx, path, oldLayout, oldData != nil)
	if err != nil || action == "status" {
		return before, err
	}
	if action == "linger-on" || action == "linger-off" {
		op := "enable-linger"
		if action == "linger-off" {
			op = "disable-linger"
		}
		if _, err := managerCommand(ctx, "/usr/bin/loginctl", op, strconv.Itoa(os.Geteuid())); err != nil {
			return before, err
		}
		return readServiceState(ctx, path, oldLayout, oldData != nil)
	}
	if action == "register" || action == "replace" || action == "unregister" {
		if action == "register" && oldData != nil {
			if oldLayout == layout {
				return before, nil
			}
			return before, ErrServiceConflict
		}
		if action == "unregister" && oldData == nil {
			return before, nil
		}
		var next []byte
		if action != "unregister" {
			if layout.Mode != productlayout.ModePortable && !productlayout.RPMManaged(layout) {
				return before, ErrServiceUnavailable
			}
			info, err := os.Stat(filepath.Join(layout.ExecutableDirectory, "sidraviad"))
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return before, ErrServiceUnavailable
			}
			next, err = registration.Encode(layout)
			if err != nil {
				return before, err
			}
		}
		apply := func() (err error) {
			if !before.Registered && next != nil {
				release, err := userlease.Acquire()
				if err != nil {
					return errors.Join(ErrServiceConflict, err)
				}
				defer func() { err = errors.Join(err, release()) }()
			}
			return replaceRegistration(ctx, path, before, oldLayout, oldData, next, layout, ready)
		}
		if err := apply(); err != nil {
			observe, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			lastLayout, lastData, _ := registration.Read(path)
			actual, observationErr := readServiceState(observe, path, lastLayout, lastData != nil)
			return actual, errors.Join(err, observationErr)
		}
		if next == nil {
			layout = productlayout.Layout{}
		}
		return readServiceState(ctx, path, layout, next != nil)
	}
	if oldData == nil || oldLayout.ExecutableDirectory != layout.ExecutableDirectory {
		return before, ErrServiceConflict
	}
	switch action {
	case "start", "stop", "enable", "disable", "restart", "recover-start":
	default:
		return before, ErrServiceUnavailable
	}
	startedHere := false
	switch action {
	case "stop":
		err = stopService(ctx, before.Running)
	case "start", "recover-start":
		if !before.Running {
			err = startService(ctx)
			startedHere = err == nil
		}
	case "restart":
		if err = stopService(ctx, before.Running); err == nil {
			err = startService(ctx)
			startedHere = err == nil
		}
	default:
		_, err = control(ctx, action, registration.UnitName)
	}
	if err == nil && ready != nil && (action == "start" || action == "restart" || action == "recover-start") {
		err = ready(ctx, oldLayout)
		if err != nil && startedHere {
			finish, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, stopErr := control(finish, "stop", registration.UnitName)
			err = errors.Join(err, stopErr)
		}
	}
	actual, observeErr := readServiceState(ctx, path, oldLayout, true)
	return actual, errors.Join(err, observeErr)
}

func replaceRegistration(ctx context.Context, path string, before ServiceState, oldLayout productlayout.Layout, oldData, next []byte, layout productlayout.Layout, ready func(context.Context, productlayout.Layout) error) (resultErr error) {
	apply := func(ctx context.Context, data []byte, enabled bool) error {
		if err := saveUnit(path, data); err != nil {
			return err
		}
		if _, err := control(ctx, "daemon-reload"); err != nil {
			return err
		}
		if data != nil {
			if _, err := readServiceState(ctx, path, layout, true); err != nil {
				return err
			}
		}
		if data != nil && enabled {
			_, err := control(ctx, "enable", registration.UnitName)
			return err
		}
		return nil
	}
	if before.Registered {
		if err := stopService(ctx, before.Running); err != nil {
			return err
		}
	}
	defer func() {
		if resultErr == nil {
			return
		}
		restore, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, observed, err := registration.Read(path)
		if err != nil || (!bytes.Equal(observed, oldData) && !bytes.Equal(observed, next)) {
			resultErr = errors.Join(resultErr, ErrServiceConflict, err)
			return
		}
		if observed != nil {
			if _, err := control(restore, "disable", "--now", registration.UnitName); err != nil {
				resultErr = errors.Join(resultErr, err)
				return
			}
		}
		if err := apply(restore, oldData, before.Enabled); err != nil {
			resultErr = errors.Join(resultErr, err)
			return
		}
		if before.Running {
			_, err := control(restore, "reset-failed", registration.UnitName)
			if err == nil {
				_, err = control(restore, "start", registration.UnitName)
			}
			if err == nil && ready != nil {
				err = ready(restore, oldLayout)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if before.Registered {
		if _, err := control(ctx, "disable", registration.UnitName); err != nil {
			return err
		}
	}
	if err := apply(ctx, next, before.Enabled); err != nil {
		return err
	}
	if next != nil && before.Running {
		if err := startService(ctx); err != nil {
			return err
		}
		if ready != nil {
			return ready(ctx, layout)
		}
	}
	return nil
}
