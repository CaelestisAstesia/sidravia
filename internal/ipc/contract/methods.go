package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// MethodDaemonStatus is the method name for daemon status queries.
const MethodDaemonStatus = "daemon.status"

// StatusResult is the result of a daemon.status request.
type StatusResult struct {
	ProductVersion string `json:"productVersion"`
	BuildID        string `json:"buildId"`
	PID            int    `json:"pid"`
	Status         string `json:"status"`
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
	ErrorCodeUnknownMethod          = "unknown_method"
	ErrorCodeMalformed              = "malformed_request"
	ErrorCodeInvalidArgument        = "invalid_argument"
	ErrorCodeProfileNotFound        = "profile_not_found"
	ErrorCodeProtocolNotFound       = "protocol_not_found"
	ErrorCodeProfileOperationFailed = "profile_operation_failed"
	ErrorCodeSessionOperationFailed = "session_operation_failed"
)

// Method names for one-shot Session operations.
const (
	MethodSessionStartOneShot = "session.startOneShot"
	MethodSessionStop         = "session.stop"
	MethodSessionGet          = "session.get"
	MethodSessionList         = "session.list"
	MethodProfileList         = "profile.list"
)

// DecodeEmptyPayload accepts exactly one empty JSON object.
func DecodeEmptyPayload(data []byte) error {
	var payload struct{}
	if err := decodeStrict(data, &payload); err != nil {
		return err
	}
	return nil
}

// SessionStartOneShotPayload is the typed payload for a session.startOneShot
// request. The protocolContextOverride is an opaque JSON document that the
// daemon forwards to the selected authentication protocol factory; the IPC layer
// never interprets it. It deliberately omits ConfigurationID, CredentialID,
// arbitrary environment facts, factory selection and a generic parameter map.
type SessionStartOneShotPayload struct {
	DisplayName              string          `json:"displayName"`
	InstitutionProfileID     string          `json:"institutionProfileId"`
	Username                 string          `json:"username"`
	Password                 string          `json:"password"`
	NetworkBindingPolicyMode string          `json:"networkBindingPolicyMode"`
	ProtocolContextOverride  json.RawMessage `json:"protocolContextOverride"`
}

// SessionStopPayload is the typed payload for a session.stop request.
type SessionStopPayload struct {
	SessionID string `json:"sessionId"`
}

// SessionGetPayload is the typed payload for a session.get request.
type SessionGetPayload struct {
	SessionID string `json:"sessionId"`
}

// DecodeSessionStartOneShotPayload strictly decodes a session.startOneShot
// payload. It rejects unknown fields, missing required fields, null, trailing
// JSON values and trailing garbage. The password may be empty; the username,
// institution profile id, network binding policy mode and a non-null protocol
// context override are required. Returned errors never include the payload or
// password.
func DecodeSessionStartOneShotPayload(data []byte) (SessionStartOneShotPayload, error) {
	var payload SessionStartOneShotPayload
	if err := decodeStrict(data, &payload); err != nil {
		return SessionStartOneShotPayload{}, err
	}
	if payload.InstitutionProfileID == "" {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing institutionProfileId")
	}
	if payload.Username == "" {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing username")
	}
	if payload.NetworkBindingPolicyMode == "" {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing networkBindingPolicyMode")
	}
	if len(payload.ProtocolContextOverride) == 0 || string(payload.ProtocolContextOverride) == "null" {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing protocolContextOverride")
	}
	return payload, nil
}

// DecodeSessionStopPayload strictly decodes a session.stop payload.
func DecodeSessionStopPayload(data []byte) (SessionStopPayload, error) {
	var payload SessionStopPayload
	if err := decodeStrict(data, &payload); err != nil {
		return SessionStopPayload{}, err
	}
	if payload.SessionID == "" {
		return SessionStopPayload{}, fmt.Errorf("decode stop payload: missing sessionId")
	}
	return payload, nil
}

// DecodeSessionGetPayload strictly decodes a session.get payload.
func DecodeSessionGetPayload(data []byte) (SessionGetPayload, error) {
	var payload SessionGetPayload
	if err := decodeStrict(data, &payload); err != nil {
		return SessionGetPayload{}, err
	}
	if payload.SessionID == "" {
		return SessionGetPayload{}, fmt.Errorf("decode get payload: missing sessionId")
	}
	return payload, nil
}

// decodeStrict decodes exactly one JSON value into target while rejecting
// unknown fields, null or empty input, trailing JSON values and trailing
// garbage. It mirrors the strictness of DecodeRequest for inner payloads.
func decodeStrict(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return fmt.Errorf("decode payload: missing payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("decode payload: trailing data")
	} else if err != io.EOF {
		return fmt.Errorf("decode payload: trailing garbage")
	}
	return nil
}

// SessionStateReason is the public reason a Session entered its current state.
type SessionStateReason struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

// SessionNetworkBinding is the public summary of the network binding a Session
// selected.
type SessionNetworkBinding struct {
	InterfaceID      string `json:"interfaceId"`
	DisplayName      string `json:"displayName"`
	LocalIPv4Address string `json:"localIpv4Address"`
}

// SessionAuthenticationFailure is the public summary of the last authentication
// failure. It never carries the wrapped diagnostic cause.
type SessionAuthenticationFailure struct {
	Code                   string `json:"code"`
	Description            string `json:"description"`
	HandlingRecommendation string `json:"handlingRecommendation"`
}

// SessionResult is the complete public result of a Session operation. It covers
// every public session.Snapshot field without exposing CredentialID, username,
// password, raw protocol configuration, the raw protocol context override or
// diagnostic causes.
type SessionResult struct {
	AuthenticationSessionID     string                        `json:"sessionId"`
	DisplayName                 string                        `json:"displayName"`
	InstitutionProfileID        string                        `json:"institutionProfileId"`
	InstitutionDisplayName      string                        `json:"institutionDisplayName"`
	AuthenticationProtocolID    string                        `json:"authenticationProtocolId"`
	AccountLabel                string                        `json:"accountLabel"`
	Intent                      string                        `json:"intent"`
	State                       string                        `json:"state"`
	StateReason                 *SessionStateReason           `json:"stateReason,omitempty"`
	SelectedNetworkBinding      *SessionNetworkBinding        `json:"selectedNetworkBinding,omitempty"`
	AuthenticationEstablishedAt *string                       `json:"authenticationEstablishedAt,omitempty"`
	NextRetryAt                 *string                       `json:"nextRetryAt,omitempty"`
	LastAuthenticationFailure   *SessionAuthenticationFailure `json:"lastAuthenticationFailure,omitempty"`
	Revision                    uint64                        `json:"revision"`
	UpdatedAt                   string                        `json:"updatedAt"`
}

// MarshalSessionResult encodes a SessionResult as JSON.
func MarshalSessionResult(result SessionResult) (json.RawMessage, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

type SessionListResult struct {
	Sessions []SessionResult `json:"sessions"`
}

func MarshalSessionListResult(result SessionListResult) (json.RawMessage, error) {
	if result.Sessions == nil {
		result.Sessions = []SessionResult{}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

type ProfileSummaryResult struct {
	InstitutionProfileID     string `json:"institutionProfileId"`
	DisplayName              string `json:"displayName"`
	AuthenticationProtocolID string `json:"authenticationProtocolId"`
}

type ProfileListResult struct {
	Profiles []ProfileSummaryResult `json:"profiles"`
}

func MarshalProfileListResult(result ProfileListResult) (json.RawMessage, error) {
	if result.Profiles == nil {
		result.Profiles = []ProfileSummaryResult{}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
