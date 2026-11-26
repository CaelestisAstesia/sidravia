// Package update owns platform-independent update decisions. It does not own
// authentication state or install files; application and platform adapters do.
package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
)

var (
	ErrInvalid    = change.ErrInvalid
	ErrNotApplied = errors.New("update is not confirmed applied")
	ErrExpired    = errors.New("update recovery intent expired")
	ErrConsumed   = errors.New("update recovery intent already claimed")
)

type Target = change.Target
type RestoreIntent = maintenance.Intent

type ResumeTicket struct {
	OperationID string        `json:"operationId"`
	Target      Target        `json:"target"`
	ExpiresAt   time.Time     `json:"expiresAt"`
	Intent      RestoreIntent `json:"intent"`
}

type RestorePlan struct {
	Mode            string `json:"mode"`
	Visible         bool   `json:"visible"`
	SkipAutoLogin   bool   `json:"skipAutoLogin"`
	ConfigurationID string `json:"configurationId,omitempty"`
}

func ValidOperationID(id string) bool { return change.ValidID(id) }

// PlanRestore never chooses a configuration or infers an intent from autostart
// preferences. Applied is supplied only after the executor verifies its work.
func PlanRestore(ticket ResumeTicket, actual Target, applied bool, now time.Time) (RestorePlan, error) {
	if !applied {
		return RestorePlan{}, ErrNotApplied
	}
	if !ValidOperationID(ticket.OperationID) || ticket.Target.Validate() != nil || ticket.Target != actual {
		return RestorePlan{}, ErrInvalid
	}
	if !now.Before(ticket.ExpiresAt) || ticket.ExpiresAt.Sub(now) > 30*time.Minute {
		return RestorePlan{}, ErrExpired
	}
	i := ticket.Intent
	if i.Mode != "none" && i.Mode != "desktop" && i.Mode != "headless" && i.Mode != "systemd" {
		return RestorePlan{}, ErrInvalid
	}
	if (i.Visible && i.Mode != "desktop") || (i.Maintain != (i.ConfigurationID != "")) || ((i.Mode == "none" || i.Mode == "systemd") && i.Maintain) {
		return RestorePlan{}, ErrInvalid
	}
	if len(i.ConfigurationID) > 128 {
		return RestorePlan{}, ErrInvalid
	}
	return RestorePlan{Mode: i.Mode, Visible: i.Visible, SkipAutoLogin: i.Mode != "none" && i.Mode != "systemd", ConfigurationID: i.ConfigurationID}, nil
}

// ClaimRestore requires an already secured operation directory. A durable
// exclusive claim makes recovery at-most-once, even if the consumer crashes.
// A damaged/partial existing claim fails closed; callers may offer manual retry.
func ClaimRestore(directory string, ticket ResumeTicket, actual Target, applied bool, now time.Time) (RestorePlan, error) {
	plan, err := PlanRestore(ticket, actual, applied, now)
	if err != nil {
		return RestorePlan{}, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return RestorePlan{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return RestorePlan{}, ErrInvalid
	}
	claim := filepath.Join(directory, ticket.OperationID+".claimed")
	f, err := os.OpenFile(claim, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return RestorePlan{}, ErrConsumed
	}
	if err != nil {
		return RestorePlan{}, fmt.Errorf("claim recovery: %w", err)
	}
	_, writeErr := f.WriteString("claimed\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return RestorePlan{}, err
	}
	return plan, nil
}
