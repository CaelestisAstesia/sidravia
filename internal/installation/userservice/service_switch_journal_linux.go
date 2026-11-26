//go:build linux

package userservice

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/internal/integrationfile"
	"sidravia/internal/installation/userservice/registration"
)

func switchPath(unit string) string {
	return filepath.Join(filepath.Dir(unit), ".sidravia-switch.json")
}

func pendingServiceSwitch() (ServiceSwitch, bool, error) {
	if os.Geteuid() == 0 {
		return ServiceSwitch{}, false, nil
	}
	path, err := registration.UnitPath()
	if err != nil {
		return ServiceSwitch{}, false, err
	}
	return loadServiceSwitch(switchPath(path))
}

func loadServiceSwitch(path string) (ServiceSwitch, bool, error) {
	var state ServiceSwitch
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return state, false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return state, false, ErrServiceConflict
	}
	if info.Size() > 16384 {
		return state, false, ErrServiceJournalDamaged
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil {
		return state, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || !validServiceSwitch(state) {
		return ServiceSwitch{}, false, ErrServiceJournalDamaged
	}
	return state, true, nil
}

func discardDamagedServiceSwitch(path string) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if _, _, err := loadServiceSwitch(path); !errors.Is(err, ErrServiceJournalDamaged) {
		return errors.Join(ErrServiceJournal, err)
	}
	now, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, now) {
		return errors.Join(ErrServiceConflict, err)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func validServiceSwitch(state ServiceSwitch) bool {
	raw, err := hex.DecodeString(state.ID)
	if err != nil || len(raw) != 16 || hex.EncodeToString(raw) != state.ID ||
		!filepath.IsAbs(state.Layout.ExecutableDirectory) || filepath.Clean(state.Layout.ExecutableDirectory) != state.Layout.ExecutableDirectory ||
		!filepath.IsAbs(state.Layout.ConfigurationsPath) || !filepath.IsAbs(state.Layout.RuntimeInfoPath) ||
		!state.Layout.Namespace.IsProduction() || len(state.ConfigurationID) > 128 || state.Expires.IsZero() || state.RuntimePID < 0 || state.TargetOwnerPID < 0 || (state.TargetOwnerPID == 0) != (state.TargetOwnerStart == 0) {
		return false
	}
	if state.Mode == "replacement" {
		return state.OwnerPID == 0 && state.OwnerStart == 0 && state.TargetOwnerPID == 0 &&
			state.ConfigurationID == "" && filepath.IsAbs(state.TargetDirectory) &&
			filepath.Clean(state.TargetDirectory) == state.TargetDirectory && state.TargetDirectory != state.Layout.ExecutableDirectory &&
			(state.RuntimePID == 0 && state.SourceVersion == "" && state.SourceBuild == "" ||
				state.RuntimePID > 0 && state.SourceVersion != "" && state.SourceBuild != "")
	}
	if state.RuntimePID == 0 || state.TargetDirectory != "" || state.SourceVersion != "" || state.SourceBuild != "" {
		return false
	}
	switch state.Mode {
	case "systemd", "headless":
		return state.OwnerPID == 0 && state.OwnerStart == 0
	case "desktop":
		return state.OwnerPID > 0 && state.OwnerStart != 0
	}
	return false
}

func saveServiceSwitchAt(unit string, state *ServiceSwitch) error {
	path := switchPath(unit)
	var data []byte
	if state != nil {
		if !validServiceSwitch(*state) {
			return ErrServiceConflict
		}
		if _, exists, err := loadServiceSwitch(path); exists || err != nil {
			return errors.Join(ErrServiceConflict, err)
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		data = encoded
	}
	if err := integrationfile.Save(path, data); err != nil {
		return registrationFileError(err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func reloadServiceSwitchAt(ctx context.Context, unit string) error {
	if _, found, err := loadServiceSwitch(switchPath(unit)); err != nil || !found {
		return errors.Join(ErrServiceRecovery, err)
	}
	_, err := control(ctx, "daemon-reload")
	return err
}
