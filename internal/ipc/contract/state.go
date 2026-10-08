package contract

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"unicode/utf8"
)

const (
	MethodStateSubscribe      = "state.subscribe"
	MethodStateUnsubscribe    = "state.unsubscribe"
	EventMethodSessionChanged = "session.changed"
	EventMethodSessionRemoved = "session.removed"
	EventMethodNetworkChanged = "network.changed"
	KindEvent                 = "event"
	StateResourceCapacity     = 256
	StateFrameLimit           = 64 * 1024
)

var (
	ErrStateFrameLimit       = errors.New("state frame limit exceeded")
	ErrStateResourceCapacity = errors.New("state resource capacity exceeded")
)

const (
	ErrorCodeStateSnapshotTooLarge    = "state_snapshot_too_large"
	ErrorCodeStateSnapshotUnavailable = "state_snapshot_unavailable"
)

type StateUnsubscribeResult struct {
	Status string `json:"status"`
}

func DecodeStateUnsubscribeResult(data []byte) (StateUnsubscribeResult, error) {
	if err := networkObject(data, []string{"status"}); err != nil {
		return StateUnsubscribeResult{}, err
	}
	var value StateUnsubscribeResult
	if err := decodeStrict(data, &value); err != nil {
		return StateUnsubscribeResult{}, err
	}
	if value.Status != "unsubscribed" {
		return StateUnsubscribeResult{}, errors.New("invalid state unsubscribe status")
	}
	return value, nil
}

func MarshalStateUnsubscribeResult(value StateUnsubscribeResult) (json.RawMessage, error) {
	if value.Status != "unsubscribed" {
		return nil, errors.New("invalid state unsubscribe status")
	}
	return json.Marshal(value)
}

// StateEventSource is the Application boundary used by the IPC server.
// Callers own the returned stream and must Close it, including on disconnect.
type StateEventSource interface {
	SubscribeStateEvents(context.Context) (StateBootstrap, StateEventStream, error)
}

type StateEventStream interface {
	Next(context.Context) (StateEvent, error)
	Close() error
}

// Bootstrap composes sequential owner queries; it is not a global atomic snapshot.
type StateBootstrap struct {
	Sessions SessionListResult       `json:"sessions"`
	Network  NetworkInterfacesResult `json:"network"`
}

type SessionChangedPayload struct {
	Session         SessionResult `json:"session"`
	CleanupRequired bool          `json:"cleanupRequired"`
}

type SessionRemovedPayload struct {
	SessionID string `json:"sessionId"`
	Revision  uint64 `json:"revision"`
}

// StateEvent has exactly one payload, selected by Method. It has no response ID.
type StateEvent struct {
	Method         string
	SessionChanged *SessionChangedPayload
	SessionRemoved *SessionRemovedPayload
	NetworkChanged *NetworkInterfacesResult
}

func DecodeStateBootstrap(data []byte) (StateBootstrap, error) {
	if len(data) > StateFrameLimit {
		return StateBootstrap{}, ErrStateFrameLimit
	}
	if err := networkObject(data, []string{"sessions", "network"}); err != nil {
		return StateBootstrap{}, err
	}
	var value StateBootstrap
	if err := decodeStrict(data, &value); err != nil {
		return StateBootstrap{}, err
	}
	if len(value.Sessions.Sessions)+1 > StateResourceCapacity {
		return StateBootstrap{}, ErrStateResourceCapacity
	}
	return value, nil
}

func MarshalStateBootstrap(value StateBootstrap) (json.RawMessage, error) {
	if len(value.Sessions.Sessions)+1 > StateResourceCapacity {
		return nil, ErrStateResourceCapacity
	}
	if !stateValidStrings(reflect.ValueOf(value)) {
		return nil, errors.New("invalid state Unicode")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = DecodeStateBootstrap(data)
	return data, err
}

func (value *SessionChangedPayload) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"session", "cleanupRequired"}); err != nil {
		return err
	}
	type plain SessionChangedPayload
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	*value = SessionChangedPayload(wire)
	return nil
}

func (value *SessionRemovedPayload) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"sessionId", "revision"}); err != nil {
		return err
	}
	type plain SessionRemovedPayload
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.SessionID == "" || wire.Revision == 0 {
		return errors.New("invalid removed Session")
	}
	*value = SessionRemovedPayload(wire)
	return nil
}

func EncodeStateEvent(value StateEvent) (json.RawMessage, error) {
	var payload any
	switch {
	case value.Method == EventMethodSessionChanged && value.SessionChanged != nil && value.SessionRemoved == nil && value.NetworkChanged == nil:
		payload = value.SessionChanged
	case value.Method == EventMethodSessionRemoved && value.SessionChanged == nil && value.SessionRemoved != nil && value.NetworkChanged == nil:
		payload = value.SessionRemoved
	case value.Method == EventMethodNetworkChanged && value.SessionChanged == nil && value.SessionRemoved == nil && value.NetworkChanged != nil:
		payload = value.NetworkChanged
	default:
		return nil, errors.New("invalid state event union")
	}
	if !stateValidStrings(reflect.ValueOf(payload)) {
		return nil, errors.New("invalid state Unicode")
	}
	data, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		Method  string `json:"method"`
		Payload any    `json:"payload"`
	}{KindEvent, value.Method, payload})
	if err != nil {
		return nil, err
	}
	_, err = DecodeStateEvent(data)
	return data, err
}

func DecodeStateEvent(data []byte) (StateEvent, error) {
	if len(data) > StateFrameLimit {
		return StateEvent{}, ErrStateFrameLimit
	}
	if err := networkObject(data, []string{"kind", "method", "payload"}); err != nil {
		return StateEvent{}, err
	}
	var wire struct {
		Kind    string          `json:"kind"`
		Method  string          `json:"method"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return StateEvent{}, err
	}
	if wire.Kind != KindEvent {
		return StateEvent{}, errors.New("invalid state event kind")
	}
	value := StateEvent{Method: wire.Method}
	switch wire.Method {
	case EventMethodSessionChanged:
		value.SessionChanged = new(SessionChangedPayload)
		if err := decodeStrict(wire.Payload, value.SessionChanged); err != nil {
			return StateEvent{}, err
		}
	case EventMethodSessionRemoved:
		value.SessionRemoved = new(SessionRemovedPayload)
		if err := decodeStrict(wire.Payload, value.SessionRemoved); err != nil {
			return StateEvent{}, err
		}
	case EventMethodNetworkChanged:
		network, err := DecodeNetworkInterfacesResult(wire.Payload)
		if err != nil {
			return StateEvent{}, err
		}
		if !network.Available {
			return StateEvent{}, errors.New("unavailable network event")
		}
		value.NetworkChanged = &network
	default:
		return StateEvent{}, errors.New("unknown state event method")
	}
	return value, nil
}

// Marshal must reject malformed Go strings before encoding/json can replace them.
func stateValidStrings(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.String:
		return utf8.ValidString(value.String())
	case reflect.Pointer, reflect.Interface:
		return value.IsNil() || stateValidStrings(value.Elem())
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if !stateValidStrings(value.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if !stateValidStrings(value.Index(i)) {
				return false
			}
		}
	}
	return true
}
