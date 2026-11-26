package userservice

import (
	"context"
	"errors"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/userservice/registration"
)

var ErrServiceUnavailable = registration.ErrUnavailable
var ErrServiceJournal = errors.New("user service switch record could not be read")
var ErrServiceJournalDamaged = errors.New("user service switch record is damaged")
var ErrServiceRecovery = errors.New("user service switch needs recovery")
var ErrServiceStop = errors.New("user service did not exit normally")
var ErrServiceConflict = registration.ErrConflict

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
