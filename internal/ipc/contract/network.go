package contract

import (
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

const MethodNetworkInterfaces = "network.interfaces"

// NetworkInterfacesResult exposes observation facts, never protocol or secrets.
// ObservedAt is the last changed observation, not a heartbeat timestamp.
type NetworkInterfacesResult struct {
	Available  bool                     `json:"available"`
	Revision   uint64                   `json:"revision"`
	Interfaces []NetworkInterfaceResult `json:"interfaces"`
	ObservedAt *string                  `json:"observedAt,omitempty"`
}
type NetworkInterfaceResult struct {
	InterfaceID              string                  `json:"interfaceId"`
	DisplayName              string                  `json:"displayName"`
	OperationalState         string                  `json:"operationalState"`
	PhysicalMedium           string                  `json:"physicalMedium"`
	HardwareBacked           bool                    `json:"hardwareBacked"`
	PhysicalConnectorPresent bool                    `json:"physicalConnectorPresent"`
	FilterInterface          bool                    `json:"filterInterface"`
	EndpointInterface        bool                    `json:"endpointInterface"`
	AddressAssignmentMethod  string                  `json:"addressAssignmentMethod"`
	IPv4Assignments          []NetworkIPv4Assignment `json:"ipv4Assignments"`
}
type NetworkIPv4Assignment struct {
	Address            string `json:"address"`
	PrefixLength       uint8  `json:"prefixLength"`
	AutomaticCandidate bool   `json:"automaticCandidate"`
	ExplicitBindable   bool   `json:"explicitBindable"`
}

func networkObject(data []byte, required []string, optional ...string) error {
	if err := validateNetworkUnicode(data); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(data, &fields); err != nil {
		return err
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		raw, ok := fields[key]
		if !ok || string(raw) == "null" {
			return errors.New("invalid network result field")
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, raw := range fields {
		if !allowed[key] || string(raw) == "null" {
			return errors.New("invalid network result field")
		}
	}
	return nil
}

var networkTimePattern = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$`)

func (value *NetworkInterfacesResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"available", "revision", "interfaces"}, "observedAt"); err != nil {
		return err
	}
	type plain NetworkInterfacesResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.Interfaces == nil {
		return errors.New("invalid network interface array")
	}
	if !wire.Available {
		if wire.Revision != 0 || len(wire.Interfaces) != 0 || wire.ObservedAt != nil {
			return errors.New("invalid unavailable network result")
		}
	} else {
		if wire.Revision == 0 || wire.ObservedAt == nil || !networkTimePattern.MatchString(*wire.ObservedAt) {
			return errors.New("invalid network observation")
		}
		observed, err := time.Parse(time.RFC3339Nano, *wire.ObservedAt)
		if err != nil || observed.IsZero() {
			return errors.New("invalid network observation time")
		}
		// time.Parse accepts +24:00/+00:60; require the RFC3339 clock/offset range.
		text := *wire.ObservedAt
		if text[len(text)-1] != 'Z' {
			offset := text[len(text)-6:]
			if offset[1:3] > "23" || offset[4:6] > "59" {
				return errors.New("invalid network observation offset")
			}
		}
	}
	*value = NetworkInterfacesResult(wire)
	return nil
}
func (value *NetworkInterfaceResult) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"interfaceId", "displayName", "operationalState", "physicalMedium", "hardwareBacked", "physicalConnectorPresent", "filterInterface", "endpointInterface", "addressAssignmentMethod", "ipv4Assignments"}); err != nil {
		return err
	}
	type plain NetworkInterfaceResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.InterfaceID == "" || !utf8.ValidString(wire.InterfaceID) || !utf8.ValidString(wire.DisplayName) || wire.IPv4Assignments == nil {
		return errors.New("invalid network interface")
	}
	if wire.OperationalState != "up" && wire.OperationalState != "down" {
		return errors.New("invalid network operational state")
	}
	if wire.PhysicalMedium != "wired" && wire.PhysicalMedium != "wireless" && wire.PhysicalMedium != "unknown" {
		return errors.New("invalid network physical medium")
	}
	if wire.AddressAssignmentMethod != "static" && wire.AddressAssignmentMethod != "dhcp" && wire.AddressAssignmentMethod != "unknown" {
		return errors.New("invalid network assignment method")
	}
	*value = NetworkInterfaceResult(wire)
	return nil
}
func (value *NetworkIPv4Assignment) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"address", "prefixLength", "automaticCandidate", "explicitBindable"}); err != nil {
		return err
	}
	type plain NetworkIPv4Assignment
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	address, err := netip.ParseAddr(wire.Address)
	if err != nil || !address.Is4() || address.String() != wire.Address || wire.PrefixLength > 32 {
		return errors.New("invalid network IPv4 assignment")
	}
	*value = NetworkIPv4Assignment(wire)
	return nil
}
func DecodeNetworkInterfacesResult(data []byte) (NetworkInterfacesResult, error) {
	var value NetworkInterfacesResult
	if err := decodeStrict(data, &value); err != nil {
		return NetworkInterfacesResult{}, err
	}
	return value, nil
}
func MarshalNetworkInterfacesResult(value NetworkInterfacesResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeNetworkInterfacesResult(data); err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// validateNetworkUnicode keeps the new owned result strict before encoding/json
// can replace invalid UTF-8 or unpaired UTF-16 escapes with U+FFFD. It scans all
// strings, including nested member names; JSON syntax remains decodeStrict's
// responsibility. Escaped backslashes and quotes are skipped as single escapes.
func validateNetworkUnicode(data []byte) error {
	invalid := errors.New("invalid network result Unicode")
	if !utf8.Valid(data) {
		return invalid
	}
	codeUnit := func(offset int) (uint16, bool) {
		if offset+4 > len(data) {
			return 0, false
		}
		unit, err := strconv.ParseUint(string(data[offset:offset+4]), 16, 16)
		return uint16(unit), err == nil
	}
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		if i+1 >= len(data) {
			continue
		}
		if data[i+1] != 'u' {
			i++
			continue
		}
		unit, ok := codeUnit(i + 2)
		if !ok {
			return invalid
		}
		switch {
		case unit >= 0xd800 && unit <= 0xdbff:
			if i+12 > len(data) || data[i+6] != '\\' || data[i+7] != 'u' {
				return invalid
			}
			low, ok := codeUnit(i + 8)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return invalid
			}
			i += 11
		case unit >= 0xdc00 && unit <= 0xdfff:
			return invalid
		default:
			i += 5
		}
	}
	return nil
}
