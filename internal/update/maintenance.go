package update

import "time"

// Maintenance owns the reversible application preparation, not Session state
// or file replacement. Its application host serializes access and observes the
// exact coordinator process separately.
type Maintenance struct {
	View     MaintenanceView
	deadline time.Time
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
	if !ValidOperationID(id) || m.Status().Phase != "idle" {
		return ErrInvalid
	}
	if intent.Mode != "desktop" && intent.Mode != "headless" {
		return ErrInvalid
	}
	m.View = MaintenanceView{SchemaVersion: 1, OperationID: id, Phase: "prepared", Interactive: interactive, Intent: intent}
	if intent.Mode == "desktop" {
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
	if m.View.OperationID != id || m.View.Phase == "committed" {
		return ErrInvalid
	}
	*m = Maintenance{}
	return nil
}

// Expiration is permitted only before commit. No timer can release an
// application after the executor may have begun replacing its files.
func (m *Maintenance) Expire(now time.Time, ownerAlive bool) bool {
	if m.Status().Phase == "idle" || m.View.Phase == "committed" {
		return false
	}
	if ownerAlive && now.Before(m.deadline) {
		return false
	}
	*m = Maintenance{}
	return true
}
