package update

import "time"

// Maintenance owns the reversible application preparation, not Session state
// or file replacement. Its application host serializes access and observes the
// exact coordinator process separately.
type Maintenance struct {
	View       MaintenanceView
	deadline   time.Time
	managedGUI bool
}
type MaintenanceView struct {
	SchemaVersion int           `json:"schemaVersion"`
	OperationID   string        `json:"operationId,omitempty"`
	Phase         string        `json:"phase"`
	Interactive   bool          `json:"interactive"`
	Intent        RestoreIntent `json:"intent"`
}

func (m *Maintenance) Status() MaintenanceView {
	if m.View.Phase == "" {
		return MaintenanceView{SchemaVersion: 1, Phase: "idle", Intent: RestoreIntent{Mode: "none"}}
	}
	return m.View
}
func (m *Maintenance) Prepare(id string, interactive bool, intent RestoreIntent, now time.Time) error {
	return m.prepare(id, interactive, intent, now, false)
}

func (m *Maintenance) PrepareServiceSwitch(id string, intent RestoreIntent, now time.Time) error {
	return m.prepare(id, false, intent, now, true)
}

func (m *Maintenance) PrepareManagedGUI(id string, interactive bool, now time.Time) error {
	if err := m.prepare(id, interactive, RestoreIntent{Mode: "systemd"}, now, true); err != nil {
		return err
	}
	m.View.Phase = "requested"
	m.managedGUI = true
	return nil
}

func (m *Maintenance) prepare(id string, interactive bool, intent RestoreIntent, now time.Time, switching bool) error {
	if !ValidOperationID(id) || m.Status().Phase != "idle" {
		return ErrInvalid
	}
	if intent.Mode != "desktop" && intent.Mode != "headless" && !(switching && intent.Mode == "systemd") {
		return ErrInvalid
	}
	m.View = MaintenanceView{SchemaVersion: 1, OperationID: id, Phase: "prepared", Interactive: interactive, Intent: intent}
	if intent.Mode == "desktop" && !switching {
		m.View.Phase = "requested"
	}
	m.deadline = now.Add(90 * time.Second)
	return nil
}
func (m *Maintenance) Acknowledge(id string, accepted, visible bool) error {
	if m.View.OperationID != id || m.View.Phase != "requested" {
		return ErrInvalid
	}
	m.View.Phase = "rejected"
	if accepted {
		m.View.Phase = "prepared"
		m.View.Intent.Visible = visible
	}
	return nil
}
func (m *Maintenance) Commit(id string) error {
	if m.View.OperationID != id || m.View.Phase != "prepared" {
		return ErrInvalid
	}
	m.View.Phase = "committed"
	return nil
}
func (m *Maintenance) Abort(id string) error {
	if m.View.OperationID != id || m.View.Phase == "committed" && !m.managedGUI {
		return ErrInvalid
	}
	*m = Maintenance{}
	return nil
}

// Expiration is permitted only before commit. No timer can release an
// application after the executor may have begun replacing its files.
func (m *Maintenance) Expire(now time.Time, ownerAlive bool) bool {
	if m.Status().Phase == "idle" || m.View.Phase == "committed" && !m.managedGUI {
		return false
	}
	if ownerAlive && now.Before(m.deadline) {
		return false
	}
	*m = Maintenance{}
	return true
}
