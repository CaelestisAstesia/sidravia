package d520

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

// institutionConfig is the validated, immutable decoding of a Profile's
// InstitutionProtocolConfiguration. Every value the wire codec and Run need
// from the institution Profile lives here; nothing caller-owned or mutable is
// retained.
type institutionConfig struct {
	serverAddress      netip.Addr
	serverPort         uint16
	authVersion        [2]byte
	keepAliveVersion   [2]byte
	controlCheckStatus byte
	ipdog              byte
	adapterNumber      byte
	osInfo             [20]byte
	challengePadding   [15]byte

	challengeTimeout  time.Duration
	loginTimeout      time.Duration
	keepaliveTimeout  time.Duration
	logoutTimeout     time.Duration
	heartbeatInterval time.Duration

	busyMaxAttempts int
	busyBackoffMin  time.Duration
	busyBackoffMax  time.Duration
}

// decodeInstitutionProtocolConfiguration decodes one strict JSON object into a
// validated institutionConfig. It rejects nil, null, non-object, malformed,
// unknown, missing and trailing JSON, and every field-specific rule listed in
// the plan. Errors identify fields and rules but never reproduce raw JSON,
// credentials, packet bytes or digest material.
func decodeInstitutionProtocolConfiguration(raw protocol.InstitutionProtocolConfiguration) (institutionConfig, error) {
	if len(raw) == 0 {
		return institutionConfig{}, fmt.Errorf("institution protocol configuration is required")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return institutionConfig{}, fmt.Errorf("institution protocol configuration must not be null")
	}
	obj, err := decodeStrictObject(raw)
	if err != nil {
		return institutionConfig{}, fmt.Errorf("institution protocol configuration is invalid: %w", err)
	}
	if err := rejectUnknownProfileFields(obj); err != nil {
		return institutionConfig{}, err
	}

	var cfg institutionConfig

	addressStr, err := requiredString(obj, "serverAddress")
	if err != nil {
		return institutionConfig{}, err
	}
	cfg.serverAddress, err = decodeServerAddress(addressStr)
	if err != nil {
		return institutionConfig{}, err
	}

	port, err := requiredInt(obj, "serverPort")
	if err != nil {
		return institutionConfig{}, err
	}
	if port < 1 || port > 65535 {
		return institutionConfig{}, fmt.Errorf("field %q must be in range 1-65535", "serverPort")
	}
	cfg.serverPort = uint16(port)

	if cfg.authVersion, err = requiredFixedHex2(obj, "authVersionHex"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.keepAliveVersion, err = requiredFixedHex2(obj, "keepAliveVersionHex"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.controlCheckStatus, err = requiredFixedHexByte(obj, "controlCheckStatusHex"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.ipdog, err = requiredFixedHexByte(obj, "ipdogHex"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.adapterNumber, err = requiredFixedHexByte(obj, "adapterNumberHex"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.osInfo, err = requiredFixedHex20(obj, "osInfoHex"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.challengePadding, err = requiredFixedHex15(obj, "challengePaddingHex"); err != nil {
		return institutionConfig{}, err
	}

	if cfg.challengeTimeout, err = requiredPositiveDuration(obj, "challengeTimeout"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.loginTimeout, err = requiredPositiveDuration(obj, "loginTimeout"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.keepaliveTimeout, err = requiredPositiveDuration(obj, "keepaliveTimeout"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.logoutTimeout, err = requiredPositiveDuration(obj, "logoutTimeout"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.heartbeatInterval, err = requiredPositiveDuration(obj, "heartbeatInterval"); err != nil {
		return institutionConfig{}, err
	}

	attempts, err := requiredInt(obj, "busyMaxAttempts")
	if err != nil {
		return institutionConfig{}, err
	}
	if attempts < 1 {
		return institutionConfig{}, fmt.Errorf("field %q must be at least 1", "busyMaxAttempts")
	}
	cfg.busyMaxAttempts = int(attempts)

	if cfg.busyBackoffMin, err = requiredPositiveDuration(obj, "busyBackoffMin"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.busyBackoffMax, err = requiredPositiveDuration(obj, "busyBackoffMax"); err != nil {
		return institutionConfig{}, err
	}
	if cfg.busyBackoffMin > cfg.busyBackoffMax {
		return institutionConfig{}, fmt.Errorf("field %q must not exceed %q", "busyBackoffMin", "busyBackoffMax")
	}

	return cfg, nil
}

// decodeStrictObject decodes raw as one JSON object with no unknown fields and
// no trailing data. A non-object value, malformed input or trailing JSON is an
// error.
func decodeStrictObject(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("must not be null")
	}
	if dec.More() {
		return nil, fmt.Errorf("has trailing JSON")
	}
	return obj, nil
}

// rejectUnknownProfileFields rejects any decoded field that is not one of the
// required Profile fields consumed by this decoder. The error names the
// unknown field but never reproduces its value or the raw JSON.
func rejectUnknownProfileFields(obj map[string]json.RawMessage) error {
	for name := range obj {
		if !isAllowedProfileField(name) {
			return fmt.Errorf("field %q is not allowed", name)
		}
	}
	return nil
}

// isAllowedProfileField reports whether name is one of the required Profile
// fields. It is a switch so no package-level mutable map is needed to store the
// field names.
func isAllowedProfileField(name string) bool {
	switch name {
	case "serverAddress",
		"serverPort",
		"authVersionHex",
		"keepAliveVersionHex",
		"controlCheckStatusHex",
		"ipdogHex",
		"adapterNumberHex",
		"osInfoHex",
		"challengePaddingHex",
		"challengeTimeout",
		"loginTimeout",
		"keepaliveTimeout",
		"logoutTimeout",
		"heartbeatInterval",
		"busyMaxAttempts",
		"busyBackoffMin",
		"busyBackoffMax":
		return true
	}
	return false
}

func requiredString(obj map[string]json.RawMessage, name string) (string, error) {
	value, ok := obj[name]
	if !ok {
		return "", missingField(name)
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return "", fmt.Errorf("field %q must be a JSON string", name)
	}
	return s, nil
}

func requiredInt(obj map[string]json.RawMessage, name string) (int64, error) {
	value, ok := obj[name]
	if !ok {
		return 0, missingField(name)
	}
	var number json.Number
	if err := json.Unmarshal(value, &number); err != nil {
		return 0, fmt.Errorf("field %q must be a JSON number", name)
	}
	n, err := number.Int64()
	if err != nil {
		return 0, fmt.Errorf("field %q must be an integer", name)
	}
	return n, nil
}

func requiredPositiveDuration(obj map[string]json.RawMessage, name string) (time.Duration, error) {
	s, err := requiredString(obj, name)
	if err != nil {
		return 0, err
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("field %q is not a valid duration", name)
	}
	if d <= 0 {
		return 0, fmt.Errorf("field %q must be positive", name)
	}
	return d, nil
}

func requiredFixedHex(obj map[string]json.RawMessage, name string, width int) ([]byte, error) {
	s, err := requiredString(obj, name)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("field %q is not valid hexadecimal", name)
	}
	if len(b) != width {
		return nil, fmt.Errorf("field %q is %d bytes, expected %d", name, len(b), width)
	}
	return b, nil
}

func requiredFixedHexByte(obj map[string]json.RawMessage, name string) (byte, error) {
	b, err := requiredFixedHex(obj, name, 1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func requiredFixedHex2(obj map[string]json.RawMessage, name string) ([2]byte, error) {
	b, err := requiredFixedHex(obj, name, 2)
	if err != nil {
		return [2]byte{}, err
	}
	var out [2]byte
	copy(out[:], b)
	return out, nil
}

func requiredFixedHex20(obj map[string]json.RawMessage, name string) ([20]byte, error) {
	b, err := requiredFixedHex(obj, name, 20)
	if err != nil {
		return [20]byte{}, err
	}
	var out [20]byte
	copy(out[:], b)
	return out, nil
}

func requiredFixedHex15(obj map[string]json.RawMessage, name string) ([15]byte, error) {
	b, err := requiredFixedHex(obj, name, 15)
	if err != nil {
		return [15]byte{}, err
	}
	var out [15]byte
	copy(out[:], b)
	return out, nil
}

func decodeServerAddress(s string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("field %q is not a valid IP address", "serverAddress")
	}
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("field %q must be an IPv4 literal", "serverAddress")
	}
	if addr.IsUnspecified() {
		return netip.Addr{}, fmt.Errorf("field %q must not be an unspecified address", "serverAddress")
	}
	if addr.IsMulticast() {
		return netip.Addr{}, fmt.Errorf("field %q must not be a multicast address", "serverAddress")
	}
	return addr, nil
}

func missingField(name string) error {
	return fmt.Errorf("field %q is required", name)
}

// validateProtocolContextOverride accepts only an absent value or a JSON
// object with no fields (such as `{}` with surrounding whitespace). It rejects
// explicit null, fields, arrays, scalars, malformed input and trailing
// values. The override cannot change environment, Profile or Run-private
// state.
func validateProtocolContextOverride(raw protocol.AuthenticationProtocolContextOverride) error {
	if len(raw) == 0 {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("protocol context override must be absent or an empty object")
	}
	obj, err := decodeStrictObject(trimmed)
	if err != nil {
		return fmt.Errorf("protocol context override is invalid: %w", err)
	}
	if len(obj) != 0 {
		return fmt.Errorf("protocol context override must be an empty object")
	}
	return nil
}
