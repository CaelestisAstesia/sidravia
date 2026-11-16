package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"time"

	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
)

const (
	EnvRecoveryOperation     = launchcontract.EnvRecoveryOperation
	EnvRecoveryToken         = launchcontract.EnvRecoveryToken
	EnvRecoveryConfiguration = launchcontract.EnvRecoveryConfiguration
)

// Recovery contains launch intent only, never credentials or IPC bearer tokens.
// Its nonce is a private, finite launch admission, not an authentication secret.
type Recovery struct {
	Intent           update.RestoreIntent `json:"intent"`
	Namespace        string               `json:"namespace"`
	CoordinatorPID   int                  `json:"coordinatorPid"`
	CoordinatorStart string               `json:"coordinatorStart"`
	Nonce            string               `json:"nonce"`
	ExpiresAt        time.Time            `json:"expiresAt"`
	LogLevel         string               `json:"logLevel"`
}

func (r Recovery) validate(record Record) error {
	if r.CoordinatorPID <= 0 || r.CoordinatorPID > 1<<31-1 || len(r.CoordinatorStart) > 20 || !update.ValidOperationID(r.Nonce) || r.ExpiresAt.After(record.CreatedAt.Add(30*time.Minute)) {
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
	_, err := update.PlanRestore(update.ResumeTicket{OperationID: record.ID, Target: record.Target, Intent: r.Intent, ExpiresAt: r.ExpiresAt}, record.Target, true, record.CreatedAt)
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
	if json.Unmarshal(raw, &fields) != nil || !exactRecordFields(fields, []string{"intent", "namespace", "coordinatorPid", "coordinatorStart", "nonce", "expiresAt", "logLevel"}) {
		return false
	}
	var intent map[string]json.RawMessage
	return json.Unmarshal(fields["intent"], &intent) == nil && exactRecordFields(intent, []string{"mode", "visible", "maintain"}, "configurationId")
}
