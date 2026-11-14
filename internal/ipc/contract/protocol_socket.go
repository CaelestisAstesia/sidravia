package contract

import (
	"errors"
	"net/netip"
	"time"
)

type NetworkEndpoint struct {
	Address string `json:"address"`
	Port    uint16 `json:"port"`
}

type NetworkProtocolSocket struct {
	State          string           `json:"state"`
	RunGeneration  uint64           `json:"runGeneration"`
	UpdatedAt      *string          `json:"updatedAt,omitempty"`
	LocalEndpoint  *NetworkEndpoint `json:"localEndpoint,omitempty"`
	RemoteEndpoint *NetworkEndpoint `json:"remoteEndpoint,omitempty"`
}

func networkTime(text string) bool {
	if !networkTimePattern.MatchString(text) {
		return false
	}
	observed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || observed.IsZero() {
		return false
	}
	if text[len(text)-1] != 'Z' {
		offset := text[len(text)-6:]
		if offset[1:3] > "23" || offset[4:6] > "59" {
			return false
		}
	}
	return true
}
func networkIPv4(text string, zero, broadcast bool) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(text)
	return addr, err == nil && addr.Is4() && addr.String() == text && !addr.IsMulticast() && (zero || !addr.IsUnspecified()) && (broadcast || text != "255.255.255.255")
}
func (value *NetworkEndpoint) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"address", "port"}); err != nil {
		return err
	}
	type plain NetworkEndpoint
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if _, ok := networkIPv4(wire.Address, false, true); !ok || wire.Port == 0 {
		return errors.New("invalid network endpoint")
	}
	*value = NetworkEndpoint(wire)
	return nil
}

func (value *NetworkProtocolSocket) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"state", "runGeneration"}, "updatedAt", "localEndpoint", "remoteEndpoint"); err != nil {
		return err
	}
	type plain NetworkProtocolSocket
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.State == "not_observed" {
		if wire.LocalEndpoint != nil || wire.RemoteEndpoint != nil || (wire.RunGeneration == 0) != (wire.UpdatedAt == nil) {
			return errors.New("invalid unobserved socket")
		}
	} else {
		switch wire.State {
		case "open", "closed", "close_failed", "close_unconfirmed":
		default:
			return errors.New("invalid socket state")
		}
		if wire.RunGeneration == 0 || wire.UpdatedAt == nil || wire.LocalEndpoint == nil || wire.RemoteEndpoint == nil || wire.LocalEndpoint.Address == "255.255.255.255" {
			return errors.New("invalid observed socket")
		}
	}
	if wire.UpdatedAt != nil && !networkTime(*wire.UpdatedAt) {
		return errors.New("invalid socket time")
	}
	*value = NetworkProtocolSocket(wire)
	return nil
}
