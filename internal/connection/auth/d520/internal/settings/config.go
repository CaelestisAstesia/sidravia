package settings

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"
)

// institutionConfig is the validated, immutable decoding of a Profile's
// InstitutionProtocolConfiguration. Every value the wire codec and Run need
// from the institution Profile lives here; nothing caller-owned or mutable is
// retained.
type Institution struct {
	JluExperimental    bool
	ServerAddress      netip.Addr
	ServerPort         uint16
	LocalPort          LocalPort
	AuthVersion        [2]byte
	KeepAliveVersion   [2]byte
	ControlCheckStatus byte
	Ipdog              byte
	AdapterNumber      byte
	OsInfo             [20]byte
	ChallengePadding   [15]byte

	// loginIPDogPadding, loginDHCPPadding and loginAuthExtensionPadding are the
	// three fixed-width Login padding regions restored as explicit Profile
	// fields. They are carried by value into loginInput and written only at
	// their documented offsets.
	LoginIPDogPadding         [4]byte
	LoginDHCPPadding          [8]byte
	LoginAuthExtensionPadding [2]byte

	ChallengeTimeout  time.Duration
	LoginTimeout      time.Duration
	KeepaliveTimeout  time.Duration
	LogoutTimeout     time.Duration
	HeartbeatInterval time.Duration

	BusyMaxAttempts int
	BusyBackoffMin  time.Duration
	BusyBackoffMax  time.Duration
}

// localPortMode is the private tagged mode of the D520 local UDP port.
type LocalPortMode int

const (
	// localPortFixed binds the socket to the configured fixed port value.
	LocalPortFixed LocalPortMode = iota + 1
	// localPortSystemAssigned lets the operating system choose a free port.
	LocalPortSystemAssigned
)

// localPort is the private immutable decoding of the tagged localPort Profile
// field. It is independent from serverPort: serverPort is the remote endpoint
// port, while localPort is the source port the single connected UDP socket
// binds. It is carried by value in institutionConfig and read by the Run when
// binding its socket; it is never exposed on a public protocol interface or in
// ProtocolContextOverride.
type LocalPort struct {
	Mode  LocalPortMode
	Value uint16 // valid only when mode == localPortFixed
}

// decodeInstitutionProtocolConfiguration decodes one strict JSON object into a
// validated institutionConfig. It rejects nil, null, non-object, malformed,
// unknown, missing and trailing JSON, and every field-specific rule listed in
// the plan. Errors identify fields and rules but never reproduce raw JSON,
// credentials, packet bytes or digest material.
func DecodeInstitution(raw []byte) (Institution, error) {
	if len(raw) == 0 {
		return Institution{}, fmt.Errorf("institution protocol configuration is required")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return Institution{}, fmt.Errorf("institution protocol configuration must not be null")
	}
	obj, err := decodeStrictObject(raw)
	if err != nil {
		return Institution{}, fmt.Errorf("institution protocol configuration is invalid: %w", err)
	}
	if err := rejectUnknownProfileFields(obj); err != nil {
		return Institution{}, err
	}

	var cfg Institution
	if rawMode, exists := obj["behavior"]; exists {
		var mode string
		if json.Unmarshal(rawMode, &mode) != nil || mode != "jlu-experimental" {
			return Institution{}, fmt.Errorf("field behavior must be jlu-experimental when present")
		}
		cfg.JluExperimental = true
	}

	addressStr, err := requiredString(obj, "serverAddress")
	if err != nil {
		return Institution{}, err
	}
	cfg.ServerAddress, err = decodeServerAddress(addressStr)
	if err != nil {
		return Institution{}, err
	}

	port, err := requiredInt(obj, "serverPort")
	if err != nil {
		return Institution{}, err
	}
	if port < 1 || port > 65535 {
		return Institution{}, fmt.Errorf("field %q must be in range 1-65535", "serverPort")
	}
	cfg.ServerPort = uint16(port)

	if cfg.LocalPort, err = decodeLocalPort(obj); err != nil {
		return Institution{}, err
	}

	if cfg.AuthVersion, err = requiredFixedHex2(obj, "authVersionHex"); err != nil {
		return Institution{}, err
	}
	if cfg.KeepAliveVersion, err = requiredFixedHex2(obj, "keepAliveVersionHex"); err != nil {
		return Institution{}, err
	}
	if cfg.ControlCheckStatus, err = requiredFixedHexByte(obj, "controlCheckStatusHex"); err != nil {
		return Institution{}, err
	}
	if cfg.Ipdog, err = requiredFixedHexByte(obj, "ipdogHex"); err != nil {
		return Institution{}, err
	}
	if cfg.AdapterNumber, err = requiredFixedHexByte(obj, "adapterNumberHex"); err != nil {
		return Institution{}, err
	}
	if cfg.OsInfo, err = requiredFixedHex20(obj, "osInfoHex"); err != nil {
		return Institution{}, err
	}
	if cfg.ChallengePadding, err = requiredFixedHex15(obj, "challengePaddingHex"); err != nil {
		return Institution{}, err
	}
	if cfg.LoginIPDogPadding, err = requiredFixedHex4(obj, "loginIPDogPaddingHex"); err != nil {
		return Institution{}, err
	}
	if cfg.LoginDHCPPadding, err = requiredFixedHex8(obj, "loginDHCPPaddingHex"); err != nil {
		return Institution{}, err
	}
	if cfg.LoginAuthExtensionPadding, err = requiredFixedHex2(obj, "loginAuthExtensionPaddingHex"); err != nil {
		return Institution{}, err
	}

	if cfg.ChallengeTimeout, err = requiredPositiveDuration(obj, "challengeTimeout"); err != nil {
		return Institution{}, err
	}
	if cfg.LoginTimeout, err = requiredPositiveDuration(obj, "loginTimeout"); err != nil {
		return Institution{}, err
	}
	if cfg.KeepaliveTimeout, err = requiredPositiveDuration(obj, "keepaliveTimeout"); err != nil {
		return Institution{}, err
	}
	if cfg.LogoutTimeout, err = requiredPositiveDuration(obj, "logoutTimeout"); err != nil {
		return Institution{}, err
	}
	if cfg.HeartbeatInterval, err = requiredPositiveDuration(obj, "heartbeatInterval"); err != nil {
		return Institution{}, err
	}

	attempts, err := requiredInt(obj, "busyMaxAttempts")
	if err != nil {
		return Institution{}, err
	}
	if attempts < 1 {
		return Institution{}, fmt.Errorf("field %q must be at least 1", "busyMaxAttempts")
	}
	cfg.BusyMaxAttempts = int(attempts)

	if cfg.BusyBackoffMin, err = requiredPositiveDuration(obj, "busyBackoffMin"); err != nil {
		return Institution{}, err
	}
	if cfg.BusyBackoffMax, err = requiredPositiveDuration(obj, "busyBackoffMax"); err != nil {
		return Institution{}, err
	}
	if cfg.BusyBackoffMin > cfg.BusyBackoffMax {
		return Institution{}, fmt.Errorf("field %q must not exceed %q", "busyBackoffMin", "busyBackoffMax")
	}

	if cfg.JluExperimental && (cfg.HeartbeatInterval > time.Minute || cfg.ChallengeTimeout > 10*time.Second || cfg.LoginTimeout > 10*time.Second || cfg.KeepaliveTimeout > 10*time.Second || cfg.LogoutTimeout > 2*time.Second || cfg.BusyMaxAttempts > 3 || cfg.BusyBackoffMax > 2*time.Second) {
		return Institution{}, fmt.Errorf("experimental timing exceeds bounded policy")
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
	case "behavior", "serverAddress",
		"serverPort",
		"localPort",
		"authVersionHex",
		"keepAliveVersionHex",
		"controlCheckStatusHex",
		"ipdogHex",
		"adapterNumberHex",
		"osInfoHex",
		"challengePaddingHex",
		"loginIPDogPaddingHex",
		"loginDHCPPaddingHex",
		"loginAuthExtensionPaddingHex",
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

func requiredFixedHex4(obj map[string]json.RawMessage, name string) ([4]byte, error) {
	b, err := requiredFixedHex(obj, name, 4)
	if err != nil {
		return [4]byte{}, err
	}
	var out [4]byte
	copy(out[:], b)
	return out, nil
}

func requiredFixedHex8(obj map[string]json.RawMessage, name string) ([8]byte, error) {
	b, err := requiredFixedHex(obj, name, 8)
	if err != nil {
		return [8]byte{}, err
	}
	var out [8]byte
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

// decodeLocalPort decodes the required tagged localPort Profile field. It is a
// strict JSON object in exactly one of the forms {"mode":"fixed","value":N} or
// {"mode":"system_assigned"}. mode is required and accepts only fixed or
// system_assigned; fixed requires an integer value in 1..65535;
// system_assigned forbids value. A missing, null, non-object, malformed, or
// trailing value, an unknown nested field, a missing or unknown mode, or an
// invalid value is rejected. Errors name the field and the violated rule but
// never reproduce the raw JSON.
func decodeLocalPort(obj map[string]json.RawMessage) (LocalPort, error) {
	raw, ok := obj["localPort"]
	if !ok {
		return LocalPort{}, missingField("localPort")
	}
	nested, err := decodeStrictObject(raw)
	if err != nil {
		return LocalPort{}, fmt.Errorf("field %q is invalid: %w", "localPort", err)
	}
	for name := range nested {
		if name != "mode" && name != "value" {
			return LocalPort{}, fmt.Errorf("field %q has an unknown nested field %q", "localPort", name)
		}
	}
	modeRaw, ok := nested["mode"]
	if !ok {
		return LocalPort{}, fmt.Errorf("field %q is missing nested field %q", "localPort", "mode")
	}
	var mode string
	if err := json.Unmarshal(modeRaw, &mode); err != nil {
		return LocalPort{}, fmt.Errorf("field %q nested field %q must be a JSON string", "localPort", "mode")
	}
	switch mode {
	case "fixed":
		valueRaw, ok := nested["value"]
		if !ok {
			return LocalPort{}, fmt.Errorf("field %q mode %q requires nested field %q", "localPort", "fixed", "value")
		}
		var number json.Number
		if err := json.Unmarshal(valueRaw, &number); err != nil {
			return LocalPort{}, fmt.Errorf("field %q nested field %q must be a JSON number", "localPort", "value")
		}
		n, err := number.Int64()
		if err != nil {
			return LocalPort{}, fmt.Errorf("field %q nested field %q must be an integer", "localPort", "value")
		}
		if n < 1 || n > 65535 {
			return LocalPort{}, fmt.Errorf("field %q nested field %q must be in range 1-65535", "localPort", "value")
		}
		return LocalPort{Mode: LocalPortFixed, Value: uint16(n)}, nil
	case "system_assigned":
		if _, ok := nested["value"]; ok {
			return LocalPort{}, fmt.Errorf("field %q mode %q forbids nested field %q", "localPort", "system_assigned", "value")
		}
		return LocalPort{Mode: LocalPortSystemAssigned}, nil
	default:
		return LocalPort{}, fmt.Errorf("field %q nested field %q has an unknown mode", "localPort", "mode")
	}
}

// validateProtocolContextOverride validates only the protocol-owned reported
// context. Host-dependent final merging is checked when creating a Run.
func ValidateOverride(raw []byte) error {
	_, err := DecodeOverride(raw)
	return err
}
