package supervisor

import "errors"

// Stable supervisor error identities let the application boundary distinguish
// recoverable session-operation failures without exposing private IDs or actor
// causes over IPC.
var (
	ErrSessionNotFound       = errors.New("session not found")
	ErrActiveSessionConflict = errors.New("another active session exists")
	ErrSessionStateConflict  = errors.New("session state does not allow operation")
)
