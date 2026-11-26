package application

import (
	"time"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
)

// Maintenance owns the reversible application preparation, not Session state
// or file replacement. Its application host serializes access and observes the
// exact coordinator process separately.
type maintenanceState struct {
	view       maintenance.View
	deadline   time.Time
	managedGUI bool
}

func (m *maintenanceState) Status() maintenance.View {
	if m.view.Phase == "" {
		return maintenance.View{SchemaVersion: 1, Phase: "idle", Intent: maintenance.Intent{Mode: "none"}}
	}
	return m.view
}
func (m *maintenanceState) Prepare(id string, interactive bool, intent maintenance.Intent, now time.Time) error {
	return m.prepare(id, interactive, intent, now, false)
}

func (m *maintenanceState) PrepareServiceSwitch(id string, intent maintenance.Intent, now time.Time) error {
	return m.prepare(id, false, intent, now, true)
}

func (m *maintenanceState) PrepareManagedGUI(id string, interactive bool, now time.Time) error {
	if err := m.prepare(id, interactive, maintenance.Intent{Mode: "systemd"}, now, true); err != nil {
		return err
	}
	m.view.Phase = "requested"
	m.managedGUI = true
	return nil
}

func (m *maintenanceState) prepare(id string, interactive bool, intent maintenance.Intent, now time.Time, switching bool) error {
	if !change.ValidID(id) || m.Status().Phase != "idle" {
		return change.ErrInvalid
	}
	if intent.Mode != "desktop" && intent.Mode != "headless" && !(switching && intent.Mode == "systemd") {
		return change.ErrInvalid
	}
	m.view = maintenance.View{SchemaVersion: 1, OperationID: id, Phase: "prepared", Interactive: interactive, Intent: intent}
	if intent.Mode == "desktop" && !switching {
		m.view.Phase = "requested"
	}
	m.deadline = now.Add(90 * time.Second)
	return nil
}
func (m *maintenanceState) Acknowledge(id string, accepted, visible bool) error {
	if m.view.OperationID != id || m.view.Phase != "requested" {
		return change.ErrInvalid
	}
	m.view.Phase = "rejected"
	if accepted {
		m.view.Phase = "prepared"
		m.view.Intent.Visible = visible
	}
	return nil
}
func (m *maintenanceState) Commit(id string) error {
	if m.view.OperationID != id || m.view.Phase != "prepared" {
		return change.ErrInvalid
	}
	m.view.Phase = "committed"
	return nil
}
func (m *maintenanceState) Abort(id string) error {
	if m.view.OperationID != id || m.view.Phase == "committed" && !m.managedGUI {
		return change.ErrInvalid
	}
	*m = maintenanceState{}
	return nil
}

// Expiration is permitted only before commit. No timer can release an
// application after the executor may have begun replacing its files.
func (m *maintenanceState) Expire(now time.Time, ownerAlive bool) bool {
	if m.Status().Phase == "idle" || m.view.Phase == "committed" && !m.managedGUI {
		return false
	}
	if ownerAlive && now.Before(m.deadline) {
		return false
	}
	*m = maintenanceState{}
	return true
}
