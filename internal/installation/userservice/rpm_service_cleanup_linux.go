//go:build linux

package userservice

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"sidravia/internal/installation/internal/integrationfile"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/userservice/registration"
)

func userManagerAvailable(ctx context.Context, runtime string) (bool, error) {
	private := filepath.Join(runtime, "systemd/private")
	listening, err := socketListening(ctx, private)
	if listening || err != nil {
		return listening, err
	}
	bus := filepath.Join(runtime, "bus")
	listening, err = socketListening(ctx, bus)
	if err != nil || !listening {
		return false, err
	}
	// A separate session bus can outlive the manager. Query the bus daemon
	// for the owner without activating the systemd service.
	out, err := managerCommand(ctx, "/usr/bin/busctl", "--address=unix:path="+bus, "--timeout=2s",
		"call", "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus",
		"NameHasOwner", "s", "org.freedesktop.systemd1")
	if err != nil {
		return false, err
	}
	switch out {
	case "b true":
		return true, nil
	case "b false":
		return false, nil
	default:
		return false, ErrServiceUnavailable
	}
}

func socketListening(ctx context.Context, path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return false, ErrServiceConflict
	}
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	switch {
	case err == nil:
		return true, conn.Close()
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
		return false, nil
	default:
		return false, err
	}
}

// CleanupErasedRPM removes only a verified RPM registration and its related
// service switch record. The prior layout lets the caller clean desktop state.
func CleanupErasedRPM(ctx context.Context) (layout productlayout.Layout, resultErr error) {
	root := productlayout.RPMExecutableDirectory
	if _, err := os.Lstat(filepath.Join(root, "sidravia.rpm")); !errors.Is(err, os.ErrNotExist) {
		return layout, errors.Join(ErrServiceConflict, err)
	}
	path, err := registration.UnitPath()
	if err != nil {
		return layout, err
	}
	core, err := openServiceLease(path)
	if errors.Is(err, os.ErrNotExist) {
		return layout, nil
	}
	if err != nil {
		return layout, err
	}
	defer func() { resultErr = errors.Join(resultErr, core.close()) }()
	err = core.run(ctx, func(path string) error {
		observed, data, err := registration.Read(path)
		if err != nil {
			return err
		}
		layout = observed
		if data != nil && layout.Mode == productlayout.ModeInstalled && layout.ExecutableDirectory == root {
			if err := removeRPMRegistration(ctx, path, layout); err != nil {
				return err
			}
		}

		// A damaged or unrelated switch record cannot authorize deleting it.
		// The verified RPM unit above can still be removed under the same lease.
		switchState, switching, err := loadServiceSwitch(switchPath(path))
		if err == nil && switching && switchState.Layout.Mode == productlayout.ModeInstalled && switchState.Layout.ExecutableDirectory == root {
			return saveServiceSwitchAt(path, nil)
		}
		return nil
	})
	return layout, err
}

func removeRPMRegistration(ctx context.Context, path string, layout productlayout.Layout) error {
	online, err := userManagerAvailable(ctx, filepath.Join("/run/user", strconv.Itoa(os.Geteuid())))
	if err != nil {
		return err
	}
	if online {
		before, err := readServiceState(ctx, path, layout, true)
		if err != nil {
			return err
		}
		if err := stopService(ctx, before.Running); err != nil {
			return err
		}
	}

	linkErr := removeRPMEnableLink(path)
	unitErr := registrationFileError(integrationfile.Save(path, nil))
	if online {
		_, err = control(ctx, "daemon-reload")
	}
	return errors.Join(linkErr, unitErr, err)
}

func removeRPMEnableLink(path string) error {
	wants := filepath.Join(filepath.Dir(path), "default.target.wants")
	info, err := os.Lstat(wants)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	link := filepath.Join(wants, registration.UnitName)
	target, err := os.Readlink(link)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EINVAL) {
		return nil
	}
	if err != nil {
		return err
	}
	if target != path && target != filepath.Join("..", registration.UnitName) {
		return nil
	}
	err = os.Remove(link)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
