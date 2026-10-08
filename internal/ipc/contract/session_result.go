package contract

import (
	"encoding/json"
	"errors"
)

func (value *SessionResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"sessionId", "displayName", "institutionProfileId", "institutionDisplayName", "authenticationProtocolId", "accountName", "intent", "state", "revision", "updatedAt", "protocolSocket"}, "configurationId", "stateReason", "selectedNetworkBinding", "authenticationEstablishedAt", "nextRetryAt", "lastAuthenticationFailure"); err != nil {
		return err
	}
	type plain SessionResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.AuthenticationSessionID == "" || wire.InstitutionProfileID == "" || wire.InstitutionDisplayName == "" || wire.AuthenticationProtocolID == "" || wire.AccountName == "" || wire.Revision == 0 || !networkTime(wire.UpdatedAt) {
		return errors.New("invalid Session result")
	}
	if wire.Intent != "maintain_authentication" && wire.Intent != "suspend_authentication" {
		return errors.New("invalid Session intent")
	}
	switch wire.State {
	case "suspended", "waiting_for_network", "authenticating", "authenticated", "waiting_before_retry", "blocked_by_error", "stopping":
	default:
		return errors.New("invalid Session state")
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(data, &fields); err != nil {
		return err
	}
	if _, exists := fields["configurationId"]; exists && wire.ConfigurationID == "" {
		return errors.New("invalid Session configuration ID")
	}
	for _, field := range []struct {
		name string
		keys []string
	}{{"stateReason", []string{"code", "description"}}, {"selectedNetworkBinding", []string{"interfaceId", "displayName", "localIpv4Address"}}, {"lastAuthenticationFailure", []string{"code", "description", "handlingRecommendation"}}} {
		if raw, exists := fields[field.name]; exists {
			if err := networkObject(raw, field.keys); err != nil {
				return err
			}
			var values map[string]string
			if err := decodeStrict(raw, &values); err != nil {
				return err
			}
			for _, key := range field.keys {
				if values[key] == "" && !(field.name == "selectedNetworkBinding" && key == "displayName") {
					return errors.New("invalid Session nested value")
				}
			}
		}
	}
	for _, text := range []*string{wire.AuthenticationEstablishedAt, wire.NextRetryAt} {
		if text != nil && !networkTime(*text) {
			return errors.New("invalid Session time")
		}
	}
	*value = SessionResult(wire)
	return nil
}

func (value *SessionStartResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"outcome", "session"}); err != nil {
		return err
	}
	type plain SessionStartResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.Outcome != "created" && wire.Outcome != "already_running" && wire.Outcome != "resumed" {
		return errors.New("invalid Session outcome")
	}
	*value = SessionStartResult(wire)
	return nil
}

func (value *SessionListResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"sessions", "cleanupRequiredSessionIds"}); err != nil {
		return err
	}
	type plain SessionListResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.Sessions == nil || wire.CleanupRequiredSessionIDs == nil {
		return errors.New("invalid Session arrays")
	}
	ids := map[string]bool{}
	for _, item := range wire.Sessions {
		if ids[item.AuthenticationSessionID] {
			return errors.New("duplicate Session ID")
		}
		ids[item.AuthenticationSessionID] = true
	}
	cleanup := map[string]bool{}
	for _, id := range wire.CleanupRequiredSessionIDs {
		if id == "" || !ids[id] || cleanup[id] {
			return errors.New("invalid cleanup Session ID")
		}
		cleanup[id] = true
	}
	*value = SessionListResult(wire)
	return nil
}

func DecodeSessionResult(data []byte) (SessionResult, error) {
	var value SessionResult
	err := decodeStrict(data, &value)
	return value, err
}
func DecodeSessionStartResult(data []byte) (SessionStartResult, error) {
	var value SessionStartResult
	err := decodeStrict(data, &value)
	return value, err
}
func DecodeSessionListResult(data []byte) (SessionListResult, error) {
	var value SessionListResult
	err := decodeStrict(data, &value)
	return value, err
}
func MarshalSessionResult(value SessionResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = DecodeSessionResult(data)
	return data, err
}
func MarshalSessionStartResult(value SessionStartResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = DecodeSessionStartResult(data)
	return data, err
}
func MarshalSessionListResult(value SessionListResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = DecodeSessionListResult(data)
	return data, err
}
