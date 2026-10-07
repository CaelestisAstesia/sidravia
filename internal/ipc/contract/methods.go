package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	config "sidravia/internal/daemon/configuration"
)

// MethodDaemonStatus is the method name for daemon status queries.
const MethodDaemonStatus = "daemon.status"

// MethodDaemonStop is the method name for a committed daemon stop request.
const MethodDaemonStop = "daemon.stop"

// StatusResult is the result of a daemon.status request.
type StatusResult struct {
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

// Method names for one-shot Session operations.
const (
	MethodSessionStartOneShot       = "session.startOneShot"
	MethodSessionStop               = "session.stop"
	MethodSessionEnsureRunning      = "session.ensureRunning"
	MethodSessionRestart            = "session.restart"
	MethodSessionRemove             = "session.remove"
	MethodSessionGet                = "session.get"
	MethodSessionList               = "session.list"
	MethodProfileList               = "profile.list"
	MethodConfigurationList         = "configuration.list"
	MethodConfigurationGet          = "configuration.get"
	MethodConfigurationCreate       = "configuration.create"
	MethodConfigurationUpdate       = "configuration.update"
	MethodConfigurationSetPassword  = "configuration.setPassword"
	MethodConfigurationRemove       = "configuration.remove"
	MethodSessionStartConfiguration = "session.startConfiguration"
)

// NetworkBindingPolicy is the strict same-build wire value.
type NetworkBindingPolicy struct {
	Mode             string `json:"mode"`
	InterfaceID      string `json:"interfaceId,omitempty"`
	LocalIPv4Address string `json:"localIpv4Address,omitempty"`
}

func (policy NetworkBindingPolicy) Domain() (config.NetworkBindingPolicy, error) {
	value := config.NetworkBindingPolicy{Mode: config.NetworkBindingPolicyMode(policy.Mode), InterfaceID: policy.InterfaceID}
	if policy.LocalIPv4Address != "" {
		address, err := netip.ParseAddr(policy.LocalIPv4Address)
		if err != nil || address.String() != policy.LocalIPv4Address {
			return value, fmt.Errorf("invalid network binding policy")
		}
		value.LocalIPv4Address = address
	}
	return value, value.Validate()
}
func NetworkBindingPolicyFromDomain(policy config.NetworkBindingPolicy) NetworkBindingPolicy {
	value := NetworkBindingPolicy{Mode: string(policy.Mode), InterfaceID: policy.InterfaceID}
	if policy.LocalIPv4Address.IsValid() {
		value.LocalIPv4Address = policy.LocalIPv4Address.String()
	}
	return value
}
func (policy *NetworkBindingPolicy) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("invalid network binding policy")
	}
	fields := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("invalid network binding policy")
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("invalid network binding policy")
		}
		if _, duplicate := fields[name]; duplicate {
			return fmt.Errorf("duplicate network binding policy field")
		}
		if name != "mode" && name != "interfaceId" && name != "localIpv4Address" {
			return fmt.Errorf("unknown network binding policy field")
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return fmt.Errorf("invalid network binding policy")
		}
		var value string
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("invalid network binding policy")
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("invalid network binding policy")
	}
	candidate := NetworkBindingPolicy{Mode: fields["mode"], InterfaceID: fields["interfaceId"], LocalIPv4Address: fields["localIpv4Address"]}
	if candidate.Mode == string(config.AutomaticallySelectLatestAvailable) {
		if len(fields) != 1 {
			return fmt.Errorf("mixed network binding policy")
		}
	} else if candidate.Mode != string(config.ExplicitInterfaceAndLocalIPv4) || len(fields) != 3 {
		return fmt.Errorf("missing network binding policy target")
	}
	if _, err := candidate.Domain(); err != nil {
		return fmt.Errorf("invalid network binding policy")
	}
	*policy = candidate
	return nil
}

type ConfigurationIDPayload struct {
	ConfigurationID string `json:"configurationId"`
}
type ConfigurationCreatePayload struct {
	NetworkBindingPolicy NetworkBindingPolicy `json:"networkBindingPolicy"`
	ConfigurationID      string               `json:"configurationId,omitempty"`
	DisplayName          string               `json:"displayName,omitempty"`
	InstitutionProfileID string               `json:"institutionProfileId"`
	Username             string               `json:"username"`
	Password             string               `json:"password"`
	AllowInsecureStorage bool                 `json:"allowInsecureStorage"`
	AutoLogin            bool                 `json:"autoLogin"`
	AutoReconnect        bool                 `json:"autoReconnect"`
}
type ConfigurationUpdatePayload struct {
	NetworkBindingPolicy *NetworkBindingPolicy `json:"networkBindingPolicy,omitempty"`
	Password             *string               `json:"password,omitempty"`
	AllowInsecureStorage bool                  `json:"allowInsecureStorage,omitempty"`
	ConfigurationID      string                `json:"configurationId"`
	DisplayName          *string               `json:"displayName,omitempty"`
	InstitutionProfileID *string               `json:"institutionProfileId,omitempty"`
	Username             *string               `json:"username,omitempty"`
	AutoLogin            *bool                 `json:"autoLogin,omitempty"`
	AutoReconnect        *bool                 `json:"autoReconnect,omitempty"`
}
type ConfigurationSetPasswordPayload struct {
	ConfigurationID      string `json:"configurationId"`
	Password             string `json:"password"`
	AllowInsecureStorage bool   `json:"allowInsecureStorage"`
}
type ConfigurationResult struct {
	ConfigurationID          string               `json:"configurationId"`
	DisplayName              string               `json:"displayName"`
	InstitutionProfileID     string               `json:"institutionProfileId"`
	InstitutionDisplayName   string               `json:"institutionDisplayName"`
	AuthenticationProtocolID string               `json:"authenticationProtocolId"`
	Username                 string               `json:"username"`
	CredentialStored         bool                 `json:"credentialStored"`
	StorageProtection        string               `json:"storageProtection"`
	AutoLogin                bool                 `json:"autoLogin"`
	AutoReconnect            bool                 `json:"autoReconnect"`
	NetworkBindingPolicy     NetworkBindingPolicy `json:"networkBindingPolicy"`
}
type ConfigurationListResult struct {
	StorageProtection string                `json:"storageProtection"`
	Configurations    []ConfigurationResult `json:"configurations"`
}
type ConfigurationRemoveResult struct {
	ConfigurationID string `json:"configurationId"`
	Status          string `json:"status"`
}

func DecodeConfigurationIDPayload(data []byte) (ConfigurationIDPayload, error) {
	var value ConfigurationIDPayload
	if err := decodeStrict(data, &value); err != nil {
		return ConfigurationIDPayload{}, err
	}
	if value.ConfigurationID == "" {
		return ConfigurationIDPayload{}, fmt.Errorf("missing configurationId")
	}
	return value, nil
}
func DecodeConfigurationCreatePayload(data []byte) (ConfigurationCreatePayload, error) {
	var wire struct {
		NetworkBindingPolicy *NetworkBindingPolicy `json:"networkBindingPolicy"`
		ConfigurationID      *string               `json:"configurationId"`
		DisplayName          *string               `json:"displayName"`
		InstitutionProfileID *string               `json:"institutionProfileId"`
		Username             *string               `json:"username"`
		Password             *string               `json:"password"`
		AllowInsecureStorage *bool                 `json:"allowInsecureStorage"`
		AutoLogin            *bool                 `json:"autoLogin"`
		AutoReconnect        *bool                 `json:"autoReconnect"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return ConfigurationCreatePayload{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ConfigurationCreatePayload{}, err
	}
	for _, name := range []string{"configurationId", "displayName"} {
		if raw, present := fields[name]; present && string(raw) == "null" {
			return ConfigurationCreatePayload{}, fmt.Errorf("configuration create %s must be a string", name)
		}
	}
	if wire.NetworkBindingPolicy == nil || wire.InstitutionProfileID == nil ||
		wire.Username == nil || wire.Password == nil || wire.AllowInsecureStorage == nil || wire.AutoLogin == nil || wire.AutoReconnect == nil ||
		wire.ConfigurationID != nil && *wire.ConfigurationID == "" || *wire.InstitutionProfileID == "" || *wire.Username == "" {
		return ConfigurationCreatePayload{}, fmt.Errorf("missing required configuration field")
	}
	value := ConfigurationCreatePayload{
		NetworkBindingPolicy: *wire.NetworkBindingPolicy,
		InstitutionProfileID: *wire.InstitutionProfileID, Username: *wire.Username,
		Password: *wire.Password, AllowInsecureStorage: *wire.AllowInsecureStorage,
		AutoLogin: *wire.AutoLogin, AutoReconnect: *wire.AutoReconnect,
	}
	if wire.ConfigurationID != nil {
		value.ConfigurationID = *wire.ConfigurationID
	}
	if wire.DisplayName != nil {
		value.DisplayName = *wire.DisplayName
	}
	return value, nil
}

type ConfigurationRemovePayload struct {
	ConfigurationID      string `json:"configurationId"`
	AllowInsecureStorage bool   `json:"allowInsecureStorage,omitempty"`
}

func DecodeConfigurationRemovePayload(data []byte) (ConfigurationRemovePayload, error) {
	var value ConfigurationRemovePayload
	if err := decodeStrict(data, &value); err != nil {
		return value, err
	}
	if value.ConfigurationID == "" {
		return value, fmt.Errorf("missing configurationId")
	}
	if err := rejectNullFields(data, "allowInsecureStorage"); err != nil {
		return value, err
	}
	return value, nil
}
func rejectNullFields(data []byte, names ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range names {
		if raw, ok := fields[name]; ok && string(raw) == "null" {
			return fmt.Errorf("null optional configuration field")
		}
	}
	return nil
}
func DecodeConfigurationUpdatePayload(data []byte) (ConfigurationUpdatePayload, error) {
	var value ConfigurationUpdatePayload
	if err := decodeStrict(data, &value); err != nil {
		return ConfigurationUpdatePayload{}, err
	}
	if err := rejectNullFields(data, "displayName", "institutionProfileId", "username", "password", "autoLogin", "autoReconnect", "allowInsecureStorage", "networkBindingPolicy"); err != nil {
		return ConfigurationUpdatePayload{}, err
	}
	if value.ConfigurationID == "" || value.DisplayName == nil && value.InstitutionProfileID == nil && value.Username == nil && value.AutoLogin == nil && value.AutoReconnect == nil && value.Password == nil && value.NetworkBindingPolicy == nil {
		return ConfigurationUpdatePayload{}, fmt.Errorf("missing configuration update")
	}
	if value.InstitutionProfileID != nil && *value.InstitutionProfileID == "" || value.Username != nil && *value.Username == "" {
		return ConfigurationUpdatePayload{}, fmt.Errorf("empty required configuration field")
	}
	return value, nil
}
func DecodeConfigurationSetPasswordPayload(data []byte) (ConfigurationSetPasswordPayload, error) {
	var wire struct {
		ConfigurationID      *string `json:"configurationId"`
		Password             *string `json:"password"`
		AllowInsecureStorage *bool   `json:"allowInsecureStorage"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return ConfigurationSetPasswordPayload{}, err
	}
	if wire.ConfigurationID == nil || wire.Password == nil || wire.AllowInsecureStorage == nil ||
		*wire.ConfigurationID == "" {
		return ConfigurationSetPasswordPayload{}, fmt.Errorf("missing configurationId")
	}
	return ConfigurationSetPasswordPayload{
		ConfigurationID: *wire.ConfigurationID, Password: *wire.Password,
		AllowInsecureStorage: *wire.AllowInsecureStorage,
	}, nil
}
func MarshalConfigurationResult(value ConfigurationResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}
func MarshalConfigurationListResult(value ConfigurationListResult) (json.RawMessage, error) {
	if value.Configurations == nil {
		value.Configurations = []ConfigurationResult{}
	}
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}
func MarshalConfigurationRemoveResult(value ConfigurationRemoveResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}

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
// never interprets it. It deliberately omits ConfigurationID,
// arbitrary environment facts, factory selection and a generic parameter map.
type SessionStartOneShotPayload struct {
	DisplayName             string               `json:"displayName"`
	InstitutionProfileID    string               `json:"institutionProfileId"`
	Username                string               `json:"username"`
	Password                string               `json:"password"`
	NetworkBindingPolicy    NetworkBindingPolicy `json:"networkBindingPolicy"`
	ProtocolContextOverride json.RawMessage      `json:"protocolContextOverride"`
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
	if _, err := payload.NetworkBindingPolicy.Domain(); err != nil {
		return SessionStartOneShotPayload{}, fmt.Errorf("decode start payload: missing networkBindingPolicy")
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
	if err := decodeStrict(data, payload); err != nil {
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

// MarshalSessionResult encodes a SessionResult as JSON.
func MarshalSessionResult(result SessionResult) (json.RawMessage, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func MarshalSessionStartResult(result SessionStartResult) (json.RawMessage, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func MarshalSessionRemoveResult(result SessionRemoveResult) (json.RawMessage, error) {
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
