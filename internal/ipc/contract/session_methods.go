package contract

import (
	"encoding/json"
	"fmt"
)

const (
	MethodSessionStartOneShot       = "session.startOneShot"
	MethodSessionStop               = "session.stop"
	MethodSessionEnsureRunning      = "session.ensureRunning"
	MethodSessionRestart            = "session.restart"
	MethodSessionRemove             = "session.remove"
	MethodSessionGet                = "session.get"
	MethodSessionList               = "session.list"
	MethodSessionStartConfiguration = "session.startConfiguration"
)

// SessionStartOneShotPayload is the typed payload for a session.startOneShot
// request. It deliberately omits persisted configuration fields.
type SessionStartOneShotPayload struct {
	DisplayName          string               `json:"displayName"`
	InstitutionProfileID string               `json:"institutionProfileId"`
	Username             string               `json:"username"`
	Password             string               `json:"password"`
	NetworkBindingPolicy NetworkBindingPolicy `json:"networkBindingPolicy"`
}

// SessionStopPayload is the typed payload for a session.stop request.
type SessionStopPayload struct {
	SessionID string `json:"sessionId"`
}

// SessionGetPayload is the typed payload for a session.get request.
type SessionGetPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionEnsureRunningPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionRestartPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionRemovePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionRemoveResult struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

// SessionStartResult describes the effect of a start/ensure request without
// overloading the long-lived Session snapshot with operation history.
type SessionStartResult struct {
	Outcome string        `json:"outcome"`
	Session SessionResult `json:"session"`
}

// DecodeSessionStartOneShotPayload strictly decodes a session.startOneShot
// payload. It rejects unknown fields, missing required fields, null, trailing
// JSON values and trailing garbage. The password may be empty; the username,
// institution profile ID and network binding policy are required. Errors never
// include the payload or password.
func DecodeSessionStartOneShotPayload(data []byte) (SessionStartOneShotPayload, error) {
	var payload SessionStartOneShotPayload
	if err := decodeRequestPayload(data, &payload, "displayName", "institutionProfileId", "username", "password", "networkBindingPolicy"); err != nil {
		return SessionStartOneShotPayload{}, err
	}
	if payload.InstitutionProfileID == "" {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing institutionProfileId")
	}
	if payload.Username == "" {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing username")
	}
	if _, err := payload.NetworkBindingPolicy.Domain(); err != nil {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing networkBindingPolicy")
	}
	return payload, nil
}

// DecodeSessionStopPayload strictly decodes a session.stop payload.
func DecodeSessionStopPayload(data []byte) (SessionStopPayload, error) {
	var payload SessionStopPayload
	if err := decodeRequestPayload(data, &payload, "sessionId"); err != nil {
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
	if err := decodeRequestPayload(data, &payload, "sessionId"); err != nil {
		return SessionGetPayload{}, err
	}
	if payload.SessionID == "" {
		return SessionGetPayload{}, fmt.Errorf("decode get payload: missing sessionId")
	}
	return payload, nil
}

func DecodeSessionEnsureRunningPayload(data []byte) (SessionEnsureRunningPayload, error) {
	var payload SessionEnsureRunningPayload
	if err := decodeSessionIDPayload(data, &payload, &payload.SessionID); err != nil {
		return SessionEnsureRunningPayload{}, err
	}
	return payload, nil
}

func DecodeSessionRestartPayload(data []byte) (SessionRestartPayload, error) {
	var payload SessionRestartPayload
	if err := decodeSessionIDPayload(data, &payload, &payload.SessionID); err != nil {
		return SessionRestartPayload{}, err
	}
	return payload, nil
}

func DecodeSessionRemovePayload(data []byte) (SessionRemovePayload, error) {
	var payload SessionRemovePayload
	if err := decodeSessionIDPayload(data, &payload, &payload.SessionID); err != nil {
		return SessionRemovePayload{}, err
	}
	return payload, nil
}

func decodeSessionIDPayload(data []byte, payload any, sessionID *string) error {
	if err := decodeRequestPayload(data, payload, "sessionId"); err != nil {
		return err
	}
	if *sessionID == "" {
		return fmt.Errorf("decode session payload: missing sessionId")
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
// every public session.Snapshot field without exposing username,
// password, raw protocol configuration, the raw protocol context override or
// diagnostic causes.
type SessionResult struct {
	HasOverrideDocument         bool                          `json:"hasOverrideDocument,omitempty"`
	HasProtocolOverride         bool                          `json:"hasProtocolOverride,omitempty"`
	ProtocolSocket              NetworkProtocolSocket         `json:"protocolSocket"`
	AuthenticationSessionID     string                        `json:"sessionId"`
	ConfigurationID             string                        `json:"configurationId,omitempty"`
	DisplayName                 string                        `json:"displayName"`
	InstitutionProfileID        string                        `json:"institutionProfileId"`
	InstitutionDisplayName      string                        `json:"institutionDisplayName"`
	AuthenticationProtocolID    string                        `json:"authenticationProtocolId"`
	AccountName                 string                        `json:"accountName"`
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

// MarshalSessionRemoveResult encodes the successful removal receipt.
func MarshalSessionRemoveResult(result SessionRemoveResult) (json.RawMessage, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

type SessionListResult struct {
	CleanupRequiredSessionIDs []string        `json:"cleanupRequiredSessionIds"`
	Sessions                  []SessionResult `json:"sessions"`
}
