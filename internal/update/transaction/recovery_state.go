package transaction

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"time"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/installation/resume"

	packagefiles "sidravia/internal/release/files"
)

const (
	EnvRecoveryOperation     = launchcontract.EnvRecoveryOperation
	EnvRecoveryToken         = launchcontract.EnvRecoveryToken
	EnvRecoveryConfiguration = launchcontract.EnvRecoveryConfiguration
)

// Recovery contains launch intent only, never credentials or IPC bearer tokens.
// Its nonce is a private, finite launch admission, not an authentication secret.
type Recovery struct {
	SourceFilesSHA256 string             `json:"sourceFilesSha256"`
	Intent            maintenance.Intent `json:"intent"`
	Namespace         string             `json:"namespace"`
	CoordinatorPID    int                `json:"coordinatorPid"`
	CoordinatorStart  string             `json:"coordinatorStart"`
	Nonce             string             `json:"nonce"`
	IssuedAt          time.Time          `json:"issuedAt"`
	ExpiresAt         time.Time          `json:"expiresAt"`
	LogLevel          string             `json:"logLevel"`
	ManagedService    bool               `json:"managedService,omitempty"`
}

func (r Recovery) validate(record Record) error {
	if !packagefiles.ValidSHA256(r.SourceFilesSHA256) || r.CoordinatorPID <= 0 || r.CoordinatorPID > 1<<31-1 || len(r.CoordinatorStart) > 20 || !change.ValidID(r.Nonce) || r.IssuedAt.Before(record.CreatedAt) || r.IssuedAt.After(record.UpdatedAt) || r.ExpiresAt.After(r.IssuedAt.Add(30*time.Minute)) {
		return ErrInvalid
	}
	if r.ManagedService && (r.Namespace != "" || (r.Intent.Mode != "none" && r.Intent.Mode != "systemd") || record.Source.Layout != "portable") {
		return ErrInvalid
	}
	if _, err := strconv.ParseUint(r.CoordinatorStart, 10, 64); err != nil {
		return ErrInvalid
	}
	if _, err := productlayout.NewNamespace(r.Namespace); err != nil {
		return ErrInvalid
	}
	switch r.LogLevel {
	case "trace", "debug", "info", "warn", "error":
	default:
		return ErrInvalid
	}
	_, err := resume.Plan(resume.Ticket{OperationID: record.ID, Target: record.Target, Intent: r.Intent, ExpiresAt: r.ExpiresAt}, record.Target, true, r.IssuedAt)
	return err
}

// SetRecovery commits the single application-owned snapshot before preparation
// is committed. It cannot rewrite an earlier intent or resurrect a terminal op.
func (s *Store) SetRecovery(ctx context.Context, id string, recovery Recovery, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.read(ctx); err != nil {
		return err
	}
	release, err := lock(filepath.Join(s.directory, "operation.lock"))
	if err != nil {
		return err
	}
	defer release()
	r, exists, err := s.read(ctx)
	if err != nil {
		return err
	}
	if !exists || r.ID != id || r.Phase != Verified || r.Recovery != nil {
		return ErrConflict
	}
	recovery.IssuedAt = now.UTC()
	r.Recovery = &recovery
	if now.After(r.UpdatedAt) {
		r.UpdatedAt = now.UTC()
	}
	if err := r.Validate(s.keys); err != nil {
		return err
	}
	return s.write(ctx, r)
}

func recoveryShape(data []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return false
	}
	raw, exists := root["recovery"]
	if !exists {
		return true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || !exactRecordFields(fields, []string{"sourceFilesSha256", "intent", "namespace", "coordinatorPid", "coordinatorStart", "nonce", "issuedAt", "expiresAt", "logLevel"}, "managedService") {
		return false
	}
	var intent map[string]json.RawMessage
	return json.Unmarshal(fields["intent"], &intent) == nil && exactRecordFields(intent, []string{"mode", "visible", "maintain"}, "configurationId")
}
