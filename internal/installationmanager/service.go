package installationmanager

import (
	"context"
	"errors"

	productlayout "sidravia/internal/installation/layout"
)

var ErrServiceUnavailable = errors.New("user service manager unavailable")
var ErrServiceJournal = errors.New("user service switch record could not be read")
var ErrServiceJournalDamaged = errors.New("user service switch record is damaged")
var ErrServiceRecovery = errors.New("user service switch needs recovery")
var ErrServiceStop = errors.New("user service did not exit normally")
var ErrServiceConflict = errors.New("user service registration conflicts with existing state")

// ServiceState describes the effective registration and manager status. A
// registration can survive the disappearance of a portable installation.
type ServiceState struct {
	Supported             bool   `json:"supported"`
	RegistrationKnown     bool   `json:"registrationKnown"`
	Directory             string `json:"directory,omitempty"`
	Registered            bool   `json:"registered"`
	Enabled               bool   `json:"enabled"`
	Running               bool   `json:"running"`
	Linger                bool   `json:"linger"`
	CanDiscardReplacement bool   `json:"canDiscardReplacement,omitempty"`
	RecoveryPending       bool   `json:"recoveryPending,omitempty"`
	RecoveryBlocked       bool   `json:"recoveryBlocked,omitempty"`
	CanDiscardDamaged     bool   `json:"canDiscardDamaged,omitempty"`
}

// Service runs one explicit user integration operation for the caller's
// package. The readiness callback confirms the expected IPC generation.
func Service(ctx context.Context, action string, layout productlayout.Layout, ready func(context.Context, productlayout.Layout) error) (ServiceState, error) {
	return userService(ctx, action, layout, ready)
}

// RegisteredService reads the owned registration without starting the manager.
func RegisteredService() (productlayout.Layout, bool, error) {
	return registeredService()
}

// HoldServiceRegistration excludes registration mutations while a package
// transaction stops, replaces and restarts a registered daemon.
func HoldServiceRegistration(ctx context.Context) (context.Context, func(), error) {
	return holdServiceRegistration(ctx)
}

// CleanupErasedRPM removes this user's obsolete RPM registration after erase.
func CleanupErasedRPM(ctx context.Context) error {
	return cleanupErasedRPM(ctx)
}

// TrackRPMAutostart indexes custom XDG roots for offline RPM erase cleanup.
func TrackRPMAutostart(root string, enable bool) error { return trackRPMAutostart(root, enable) }

// RecoverAutostartStaging removes a private staging directory left by the GUI.
func RecoverAutostartStaging(path string) error { return recoverAutostartStaging(path) }
