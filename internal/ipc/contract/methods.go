package contract

import (
	"encoding/json"
)

// MethodDaemonStatus is the method name for daemon status queries.
const MethodDaemonStatus = "daemon.status"

// MethodDaemonStop is the method name for a committed daemon stop request.
const MethodDaemonStop = "daemon.stop"

// StatusResult is the result of a daemon.status request.
type StatusResult struct {
	LogLevel        string `json:"logLevel,omitempty"`
	ProductVersion  string `json:"productVersion"`
	BuildID         string `json:"buildId"`
	PID             int    `json:"pid"`
	Status          string `json:"status"`
	Mode            string `json:"mode"`
	DesktopOwnerPID *int   `json:"desktopOwnerPid,omitempty"`
}

// DaemonStopResult is the typed result of a daemon.stop request. The daemon
// only acknowledges that stopping has begun; the client polls until the old
// runtime generation is unreachable.
type DaemonStopResult struct {
	Status string `json:"status"`
}

// MarshalDaemonStopResult encodes a DaemonStopResult as JSON.
func MarshalDaemonStopResult(result DaemonStopResult) (json.RawMessage, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// MarshalStatusResult encodes a StatusResult as JSON.
func MarshalStatusResult(result StatusResult) (json.RawMessage, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// Error codes returned by the daemon.
const (
	ErrorCodeUnknownMethod                          = "unknown_method"
	ErrorCodeInternalError                          = "internal_error"
	ErrorCodeMalformed                              = "malformed_request"
	ErrorCodeInvalidArgument                        = "invalid_argument"
	ErrorCodeProfileNotFound                        = "profile_not_found"
	ErrorCodeProtocolNotFound                       = "protocol_not_found"
	ErrorCodeProfileOperationFailed                 = "profile_operation_failed"
	ErrorCodeSessionOperationFailed                 = "session_operation_failed"
	ErrorCodeSessionNotFound                        = "session_not_found"
	ErrorCodeSessionActiveConflict                  = "session_active_conflict"
	ErrorCodeSessionStateConflict                   = "session_state_conflict"
	ErrorCodeConfigurationNotFound                  = "configuration_not_found"
	ErrorCodeConfigurationConflict                  = "configuration_conflict"
	ErrorCodeConfigurationOperationFailed           = "configuration_operation_failed"
	ErrorCodeConfigurationSessionInvalidationFailed = "configuration_session_invalidation_failed"
	ErrorCodeInsecureStorageConfirmationRequired    = "insecure_storage_confirmation_required"
	ErrorCodeConfigurationAutoLoginConflict         = "configuration_auto_login_conflict"
)

// DecodeEmptyPayload accepts exactly one empty JSON object.
func DecodeEmptyPayload(data []byte) error {
	var payload struct{}
	if err := decodeRequestPayload(data, &payload); err != nil {
		return err
	}
	return nil
}
