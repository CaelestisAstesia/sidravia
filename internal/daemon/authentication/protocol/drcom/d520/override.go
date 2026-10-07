package d520

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"unicode/utf8"

	protocol "sidravia/internal/daemon/authentication/protocol"
	environment "sidravia/internal/daemon/environment"
)

const maxProtocolContextOverrideBytes = 16 * 1024

// reportedContextOverride contains only optional, copied packet inputs. It has
// no endpoint, transport policy, credential or transient protocol state.
type reportedContextOverride struct {
	reportedIPv4                             *[4]byte
	reportedMAC                              *[6]byte
	reportedDNSIPv4                          *[2][4]byte
	reportedDHCPIPv4                         *[4]byte
	hostName, osFamily, osRelease            *string
	authVersion, keepAliveVersion            *[2]byte
	controlCheckStatus, ipdog, adapterNumber *byte
	osInfo                                   *[20]byte
	challengePadding                         *[15]byte
	loginIPDogPadding                        *[4]byte
	loginDHCPPadding                         *[8]byte
	loginAuthExtensionPadding                *[2]byte
}

func invalidOverride() error {
	return errors.New("protocol context override must be one strict version 1 object")
}
func invalidOverrideField(name string) error {
	// name is supplied only by constant cases in the protocol whitelist.
	return fmt.Errorf("protocol context override field %q is invalid", name)
}

func decodeProtocolContextOverride(raw protocol.AuthenticationProtocolContextOverride) (reportedContextOverride, error) {
	var out reportedContextOverride
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
			return reportedContextOverride{}, invalidOverride()
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return reportedContextOverride{}, invalidOverride()
		}
		seen[name] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return reportedContextOverride{}, invalidOverride()
		}
		value = bytes.TrimSpace(value)
		if bytes.Equal(value, []byte("null")) {
			return reportedContextOverride{}, invalidOverride()
		}
		switch name {
		case "schemaVersion":
			var version int
			if json.Unmarshal(value, &version) != nil || version != 1 {
				return reportedContextOverride{}, invalidOverrideField("schemaVersion")
			}
			hasVersion = true
		case "reportedIPv4":
			addr, err := overrideIPv4(value, false)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("reportedIPv4")
			}
			out.reportedIPv4 = &addr
		case "reportedDHCPIPv4":
			addr, err := overrideIPv4(value, true)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("reportedDHCPIPv4")
			}
			out.reportedDHCPIPv4 = &addr
		case "reportedMAC":
			var text string
			if json.Unmarshal(value, &text) != nil {
				return reportedContextOverride{}, invalidOverrideField("reportedMAC")
			}
			mac, err := net.ParseMAC(text)
			if err != nil || len(mac) != 6 || mac.String() != text {
				return reportedContextOverride{}, invalidOverrideField("reportedMAC")
			}
			copied := [6]byte(mac)
			if copied == ([6]byte{}) {
				return reportedContextOverride{}, invalidOverrideField("reportedMAC")
			}
			out.reportedMAC = &copied
		case "reportedDNSIPv4":
			var entries []json.RawMessage
			if len(value) == 0 || value[0] != '[' || json.Unmarshal(value, &entries) != nil || len(entries) > 2 {
				return reportedContextOverride{}, invalidOverrideField("reportedDNSIPv4")
			}
			var dns [2][4]byte
			for i, entry := range entries {
				addr, err := overrideIPv4(entry, true)
				if err != nil {
					return reportedContextOverride{}, invalidOverrideField("reportedDNSIPv4")
				}
				dns[i] = addr
			}
			out.reportedDNSIPv4 = &dns
		case "hostName", "osFamily", "osRelease":
			var text string
			if json.Unmarshal(value, &text) != nil {
				return reportedContextOverride{}, invalidOverrideTextField(name)
			}
			encoded, err := encodeProtocolText(text)
			if err != nil || len(encoded) > 32 || (name != "hostName" && strings.TrimSpace(text) != text) {
				return reportedContextOverride{}, invalidOverrideTextField(name)
			}
			switch name {
			case "hostName":
				out.hostName = &text
			case "osFamily":
				out.osFamily = &text
			case "osRelease":
				out.osRelease = &text
			}
		case "authVersionHex":
			decoded, err := overrideFixedHex(value, 2)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("authVersionHex")
			}
			copied := [2]byte(decoded)
			out.authVersion = &copied
		case "keepAliveVersionHex":
			decoded, err := overrideFixedHex(value, 2)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("keepAliveVersionHex")
			}
			copied := [2]byte(decoded)
			out.keepAliveVersion = &copied
		case "controlCheckStatusHex":
			decoded, err := overrideFixedHex(value, 1)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("controlCheckStatusHex")
			}
			copied := decoded[0]
			out.controlCheckStatus = &copied
		case "ipdogHex":
			decoded, err := overrideFixedHex(value, 1)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("ipdogHex")
			}
			copied := decoded[0]
			out.ipdog = &copied
		case "adapterNumberHex":
			decoded, err := overrideFixedHex(value, 1)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("adapterNumberHex")
			}
			copied := decoded[0]
			out.adapterNumber = &copied
		case "osInfoHex":
			decoded, err := overrideFixedHex(value, 20)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("osInfoHex")
			}
			copied := [20]byte(decoded)
			out.osInfo = &copied
		case "challengePaddingHex":
			decoded, err := overrideFixedHex(value, 15)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("challengePaddingHex")
			}
			copied := [15]byte(decoded)
			out.challengePadding = &copied
		case "loginIPDogPaddingHex":
			decoded, err := overrideFixedHex(value, 4)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("loginIPDogPaddingHex")
			}
			copied := [4]byte(decoded)
			out.loginIPDogPadding = &copied
		case "loginDHCPPaddingHex":
			decoded, err := overrideFixedHex(value, 8)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("loginDHCPPaddingHex")
			}
			copied := [8]byte(decoded)
			out.loginDHCPPadding = &copied
		case "loginAuthExtensionPaddingHex":
			decoded, err := overrideFixedHex(value, 2)
			if err != nil {
				return reportedContextOverride{}, invalidOverrideField("loginAuthExtensionPaddingHex")
			}
			copied := [2]byte(decoded)
			out.loginAuthExtensionPadding = &copied
		default:
			return reportedContextOverride{}, invalidOverride()
		}
	}
	last, err := dec.Token()
	if err != nil || last != json.Delim('}') {
		return reportedContextOverride{}, invalidOverride()
	}
	if _, err := dec.Token(); err != io.EOF {
		return reportedContextOverride{}, invalidOverride()
	}
	if len(seen) != 0 && !hasVersion {
		return reportedContextOverride{}, invalidOverrideField("schemaVersion")
	}
	if out.osFamily != nil && out.osRelease != nil {
		host := environment.SystemHostInformation{OperatingSystemFamily: *out.osFamily, OperatingSystemRelease: *out.osRelease}
		encoded, err := encodeProtocolText(deriveHostOS(host))
		if err != nil || len(encoded) > 32 {
			return reportedContextOverride{}, errors.New("protocol context override combined host OS is invalid")
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

func (override reportedContextOverride) applyCompatibility(cfg *institutionConfig) {
	if override.authVersion != nil {
		cfg.authVersion = *override.authVersion
	}
	if override.keepAliveVersion != nil {
		cfg.keepAliveVersion = *override.keepAliveVersion
	}
	if override.controlCheckStatus != nil {
		cfg.controlCheckStatus = *override.controlCheckStatus
	}
	if override.ipdog != nil {
		cfg.ipdog = *override.ipdog
	}
	if override.adapterNumber != nil {
		cfg.adapterNumber = *override.adapterNumber
	}
	if override.osInfo != nil {
		cfg.osInfo = *override.osInfo
	}
	if override.challengePadding != nil {
		cfg.challengePadding = *override.challengePadding
	}
	if override.loginIPDogPadding != nil {
		cfg.loginIPDogPadding = *override.loginIPDogPadding
	}
	if override.loginDHCPPadding != nil {
		cfg.loginDHCPPadding = *override.loginDHCPPadding
	}
	if override.loginAuthExtensionPadding != nil {
		cfg.loginAuthExtensionPadding = *override.loginAuthExtensionPadding
	}
}
