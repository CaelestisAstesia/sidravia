package settings

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"unicode/utf8"
)

const maxProtocolContextOverrideBytes = 16 * 1024

// reportedContextOverride contains only optional, copied packet inputs. It has
// no endpoint, transport policy, credential or transient protocol state.
type Override struct {
	ReportedIPv4                             *[4]byte
	ReportedMAC                              *[6]byte
	ReportedDNSIPv4                          *[2][4]byte
	ReportedDHCPIPv4                         *[4]byte
	HostName, OsFamily, OsRelease            *string
	AuthVersion, KeepAliveVersion            *[2]byte
	ControlCheckStatus, Ipdog, AdapterNumber *byte
	OsInfo                                   *[20]byte
	ChallengePadding                         *[15]byte
	LoginIPDogPadding                        *[4]byte
	LoginDHCPPadding                         *[8]byte
	LoginAuthExtensionPadding                *[2]byte
}

type FieldError struct{ field string }

func (err FieldError) FieldName() string { return err.field }
func (err FieldError) Error() string     { return "invalid protocol override field: " + err.field }

func invalidOverride() error {
	return errors.New("protocol context override must be one strict version 1 object")
}
func invalidOverrideField(name string) error {
	// name is supplied only by constant cases in the protocol whitelist.
	return FieldError{field: name}
}

func DecodeOverride(raw []byte) (Override, error) {
	var out Override
	if len(raw) == 0 {
		return out, nil
	}
	if len(raw) > maxProtocolContextOverrideBytes || !utf8.Valid(raw) {
		return out, invalidOverride()
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return out, invalidOverride()
	}
	seen := make(map[string]bool)
	hasVersion := false
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return Override{}, invalidOverride()
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return Override{}, invalidOverride()
		}
		seen[name] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return Override{}, invalidOverride()
		}
		value = bytes.TrimSpace(value)
		if bytes.Equal(value, []byte("null")) {
			return Override{}, invalidOverride()
		}
		switch name {
		case "schemaVersion":
			var version int
			if json.Unmarshal(value, &version) != nil || version != 1 {
				return Override{}, invalidOverrideField("schemaVersion")
			}
			hasVersion = true
		case "reportedIPv4":
			addr, err := overrideIPv4(value, false)
			if err != nil {
				return Override{}, invalidOverrideField("reportedIPv4")
			}
			out.ReportedIPv4 = &addr
		case "reportedDHCPIPv4":
			addr, err := overrideIPv4(value, true)
			if err != nil {
				return Override{}, invalidOverrideField("reportedDHCPIPv4")
			}
			out.ReportedDHCPIPv4 = &addr
		case "reportedMAC":
			var text string
			if json.Unmarshal(value, &text) != nil {
				return Override{}, invalidOverrideField("reportedMAC")
			}
			mac, err := net.ParseMAC(text)
			if err != nil || len(mac) != 6 || mac.String() != text {
				return Override{}, invalidOverrideField("reportedMAC")
			}
			copied := [6]byte(mac)
			if copied == ([6]byte{}) {
				return Override{}, invalidOverrideField("reportedMAC")
			}
			out.ReportedMAC = &copied
		case "reportedDNSIPv4":
			var entries []json.RawMessage
			if len(value) == 0 || value[0] != '[' || json.Unmarshal(value, &entries) != nil || len(entries) > 2 {
				return Override{}, invalidOverrideField("reportedDNSIPv4")
			}
			var dns [2][4]byte
			for i, entry := range entries {
				addr, err := overrideIPv4(entry, true)
				if err != nil {
					return Override{}, invalidOverrideField("reportedDNSIPv4")
				}
				dns[i] = addr
			}
			out.ReportedDNSIPv4 = &dns
		case "hostName", "osFamily", "osRelease":
			var text string
			if json.Unmarshal(value, &text) != nil {
				return Override{}, invalidOverrideTextField(name)
			}
			encoded, err := EncodeText(text)
			if err != nil || len(encoded) > 32 || (name != "hostName" && strings.TrimSpace(text) != text) {
				return Override{}, invalidOverrideTextField(name)
			}
			switch name {
			case "hostName":
				out.HostName = &text
			case "osFamily":
				out.OsFamily = &text
			case "osRelease":
				out.OsRelease = &text
			}
		case "authVersionHex":
			decoded, err := overrideFixedHex(value, 2)
			if err != nil {
				return Override{}, invalidOverrideField("authVersionHex")
			}
			copied := [2]byte(decoded)
			out.AuthVersion = &copied
		case "keepAliveVersionHex":
			decoded, err := overrideFixedHex(value, 2)
			if err != nil {
				return Override{}, invalidOverrideField("keepAliveVersionHex")
			}
			copied := [2]byte(decoded)
			out.KeepAliveVersion = &copied
		case "controlCheckStatusHex":
			decoded, err := overrideFixedHex(value, 1)
			if err != nil {
				return Override{}, invalidOverrideField("controlCheckStatusHex")
			}
			copied := decoded[0]
			out.ControlCheckStatus = &copied
		case "ipdogHex":
			decoded, err := overrideFixedHex(value, 1)
			if err != nil {
				return Override{}, invalidOverrideField("ipdogHex")
			}
			copied := decoded[0]
			out.Ipdog = &copied
		case "adapterNumberHex":
			decoded, err := overrideFixedHex(value, 1)
			if err != nil {
				return Override{}, invalidOverrideField("adapterNumberHex")
			}
			copied := decoded[0]
			out.AdapterNumber = &copied
		case "osInfoHex":
			decoded, err := overrideFixedHex(value, 20)
			if err != nil {
				return Override{}, invalidOverrideField("osInfoHex")
			}
			copied := [20]byte(decoded)
			out.OsInfo = &copied
		case "challengePaddingHex":
			decoded, err := overrideFixedHex(value, 15)
			if err != nil {
				return Override{}, invalidOverrideField("challengePaddingHex")
			}
			copied := [15]byte(decoded)
			out.ChallengePadding = &copied
		case "loginIPDogPaddingHex":
			decoded, err := overrideFixedHex(value, 4)
			if err != nil {
				return Override{}, invalidOverrideField("loginIPDogPaddingHex")
			}
			copied := [4]byte(decoded)
			out.LoginIPDogPadding = &copied
		case "loginDHCPPaddingHex":
			decoded, err := overrideFixedHex(value, 8)
			if err != nil {
				return Override{}, invalidOverrideField("loginDHCPPaddingHex")
			}
			copied := [8]byte(decoded)
			out.LoginDHCPPadding = &copied
		case "loginAuthExtensionPaddingHex":
			decoded, err := overrideFixedHex(value, 2)
			if err != nil {
				return Override{}, invalidOverrideField("loginAuthExtensionPaddingHex")
			}
			copied := [2]byte(decoded)
			out.LoginAuthExtensionPadding = &copied
		default:
			return Override{}, invalidOverride()
		}
	}
	last, err := dec.Token()
	if err != nil || last != json.Delim('}') {
		return Override{}, invalidOverride()
	}
	if _, err := dec.Token(); err != io.EOF {
		return Override{}, invalidOverride()
	}
	if len(seen) != 0 && !hasVersion {
		return Override{}, invalidOverrideField("schemaVersion")
	}
	if out.OsFamily != nil && out.OsRelease != nil {
		encoded, err := EncodeText(HostOS(*out.OsFamily, *out.OsRelease))
		if err != nil || len(encoded) > 32 {
			return Override{}, invalidOverrideField("osRelease")
		}
	}
	return out, nil
}

func overrideIPv4(raw json.RawMessage, allowZero bool) ([4]byte, error) {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return [4]byte{}, invalidOverride()
	}
	addr, err := netip.ParseAddr(text)
	if err != nil || !addr.Is4() || addr.String() != text || addr.IsMulticast() || addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) || (!allowZero && addr.IsUnspecified()) {
		return [4]byte{}, invalidOverride()
	}
	return addr.As4(), nil
}

func overrideFixedHex(raw json.RawMessage, width int) ([]byte, error) {
	var text string
	if json.Unmarshal(raw, &text) != nil || len(text) != width*2 || strings.ToLower(text) != text {
		return nil, invalidOverride()
	}
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return nil, invalidOverride()
	}
	return decoded, nil
}

func invalidOverrideTextField(name string) error {
	switch name {
	case "hostName":
		return invalidOverrideField("hostName")
	case "osFamily":
		return invalidOverrideField("osFamily")
	case "osRelease":
		return invalidOverrideField("osRelease")
	default:
		return invalidOverride()
	}
}

func (override Override) ApplyCompatibility(cfg *Institution) {
	if override.AuthVersion != nil {
		cfg.AuthVersion = *override.AuthVersion
	}
	if override.KeepAliveVersion != nil {
		cfg.KeepAliveVersion = *override.KeepAliveVersion
	}
	if override.ControlCheckStatus != nil {
		cfg.ControlCheckStatus = *override.ControlCheckStatus
	}
	if override.Ipdog != nil {
		cfg.Ipdog = *override.Ipdog
	}
	if override.AdapterNumber != nil {
		cfg.AdapterNumber = *override.AdapterNumber
	}
	if override.OsInfo != nil {
		cfg.OsInfo = *override.OsInfo
	}
	if override.ChallengePadding != nil {
		cfg.ChallengePadding = *override.ChallengePadding
	}
	if override.LoginIPDogPadding != nil {
		cfg.LoginIPDogPadding = *override.LoginIPDogPadding
	}
	if override.LoginDHCPPadding != nil {
		cfg.LoginDHCPPadding = *override.LoginDHCPPadding
	}
	if override.LoginAuthExtensionPadding != nil {
		cfg.LoginAuthExtensionPadding = *override.LoginAuthExtensionPadding
	}
}
