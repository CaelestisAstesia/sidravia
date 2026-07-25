package d520

import (
	"encoding/binary"
	"fmt"
)

// Fixed packet lengths documented in docs/protocols/drcom-5.2.0-d.md.
const (
	challengeRequestLength  = 20
	challengeResponseLength = 16
	loginRequestLength      = 330
	loginSuccessLength      = 64
	loginRejectionLength    = 32
	ka1RequestLength        = 42
	ka1ResponseLength       = 20
	ka2RequestLength        = 40
	ka2ResponseLength       = 60
	logoutRequestLength     = 80
	logoutACKLength         = 4

	usernameFieldLength = 36
	hostNameFieldLength = 32
	hostOSFieldLength   = 32
	macLength           = 6
	authInfoLength      = 16

	challengePaddingLength = 15
)

// KA2 type values.
const (
	ka2Type1 byte = 1
	ka2Type3 byte = 3
)

// ka2InitVersionHi and ka2InitVersionLo are the two version bytes carried by
// the first Type1 KA2 packet. Every later KA2 carries the institution
// keep-alive version. They are immutable constants, not package-level mutable
// state.
const (
	ka2InitVersionHi byte = 0x0f
	ka2InitVersionLo byte = 0x27
)

// loginInput is the immutable, caller-supplied input for building Login and
// Logout requests. All fields are value types or strings, so a loginInput
// value can be passed by copy without sharing mutable state. Salt and Auth
// Info are not part of this value: they are owned by the Run and supplied to
// each builder separately.
type loginInput struct {
	username           string
	password           string
	mac                [6]byte
	clientIPv4         [4]byte
	hostName           string
	hostOS             string
	primaryDNS         [4]byte
	secondaryDNS       [4]byte
	dhcpIPv4           [4]byte
	osInfo             [20]byte
	controlCheckStatus byte
	adapterNum         byte
	ipdog              byte
	authVersion        [2]byte
	authExtTail        [2]byte
}

// encodeCredentialFields encodes the username, password, host name and host OS
// as protocol text and enforces the documented field bounds. It returns the
// encoded slices in packet order. Errors carry field names and lengths but
// never the input text.
func encodeCredentialFields(in loginInput) (username, password, hostName, hostOS []byte, err error) {
	if username, err = encodeProtocolText(in.username); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("username cannot be encoded as protocol text: %w", err)
	}
	if len(username) > usernameFieldLength {
		return nil, nil, nil, nil, fmt.Errorf("encoded username is %d bytes, exceeds %d", len(username), usernameFieldLength)
	}
	if password, err = encodeProtocolText(in.password); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("password cannot be encoded as protocol text: %w", err)
	}
	if hostName, err = encodeProtocolText(in.hostName); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("host name cannot be encoded as protocol text: %w", err)
	}
	if len(hostName) > hostNameFieldLength {
		return nil, nil, nil, nil, fmt.Errorf("encoded host name is %d bytes, exceeds %d", len(hostName), hostNameFieldLength)
	}
	if hostOS, err = encodeProtocolText(in.hostOS); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("host os cannot be encoded as protocol text: %w", err)
	}
	if len(hostOS) > hostOSFieldLength {
		return nil, nil, nil, nil, fmt.Errorf("encoded host os is %d bytes, exceeds %d", len(hostOS), hostOSFieldLength)
	}
	return username, password, hostName, hostOS, nil
}

// encodeLogoutCredentialFields encodes only the username and password needed
// by the Logout request. Host name and host OS are not part of Logout, so an
// unusable host field must not prevent best-effort cleanup.
func encodeLogoutCredentialFields(in loginInput) (username, password []byte, err error) {
	if username, err = encodeProtocolText(in.username); err != nil {
		return nil, nil, fmt.Errorf("username cannot be encoded as protocol text: %w", err)
	}
	if len(username) > usernameFieldLength {
		return nil, nil, fmt.Errorf("encoded username is %d bytes, exceeds %d", len(username), usernameFieldLength)
	}
	if password, err = encodeProtocolText(in.password); err != nil {
		return nil, nil, fmt.Errorf("password cannot be encoded as protocol text: %w", err)
	}
	return username, password, nil
}

// buildChallengeRequest builds the fixed 20-byte Challenge request from a
// caller-supplied seed and the exact 15-byte Profile padding.
func buildChallengeRequest(seed uint16, padding [15]byte) []byte {
	out := make([]byte, challengeRequestLength)
	out[0] = 0x01
	out[1] = 0x02
	binary.LittleEndian.PutUint16(out[2:4], seed)
	out[4] = 0x09
	copy(out[5:20], padding[:])
	return out
}

// challengeResponse carries the salt extracted from a Challenge response.
type challengeResponse struct {
	salt [4]byte
}

// parseChallengeResponse parses the 16-byte Challenge response and extracts
// its four-byte salt.
func parseChallengeResponse(resp []byte) (challengeResponse, error) {
	if len(resp) != challengeResponseLength {
		return challengeResponse{}, fmt.Errorf("challenge response is %d bytes, expected %d", len(resp), challengeResponseLength)
	}
	if resp[0] != 0x02 {
		return challengeResponse{}, fmt.Errorf("challenge response opcode 0x%02x, expected 0x02", resp[0])
	}
	var salt [4]byte
	copy(salt[:], resp[4:8])
	return challengeResponse{salt: salt}, nil
}

// buildLoginRequest builds the exact 330-byte Login request from an immutable
// private input value and the salt supplied by the Run. Construction is
// divided into private functions named for the documented wire regions; every
// offset stays explicit.
func buildLoginRequest(in loginInput, salt [4]byte) ([]byte, error) {
	username, password, hostName, hostOS, err := encodeCredentialFields(in)
	if err != nil {
		return nil, err
	}

	digestA := md5A(salt[:], password)
	macXORed, err := macXOR(in.mac, digestA)
	if err != nil {
		return nil, fmt.Errorf("compute mac xor: %w", err)
	}
	digestB := md5B(salt[:], password)
	digestC := md5C(in.clientIPv4)

	out := make([]byte, loginRequestLength)
	writeLoginHeader(out, len(username))
	writeLoginCredentialRegion(out, digestA, username, in.controlCheckStatus, in.adapterNum, macXORed, digestB)
	writeLoginIPRegion(out, in.clientIPv4, digestC, in.ipdog)
	writeLoginHostRegion(out, hostName, in.primaryDNS, in.dhcpIPv4, in.secondaryDNS, in.osInfo, hostOS)
	writeLoginAuthExtension(out, in.authVersion, in.mac, in.authExtTail)
	return out, nil
}

// writeLoginHeader writes the fixed [0,4) header and the length byte at offset
// 3, which is 20 plus the encoded username length.
func writeLoginHeader(out []byte, usernameLen int) {
	out[0] = 0x03
	out[1] = 0x01
	out[2] = 0x00
	out[3] = byte(20 + usernameLen)
}

// writeLoginCredentialRegion writes MD5-A at [4,20), the username at
// [20,20+len), control_check_status at 56, adapter_num at 57, the MAC XOR at
// [58,64) and MD5-B at [64,80).
func writeLoginCredentialRegion(out, digestA, username []byte, controlCheckStatus, adapterNum byte, macXORed, digestB []byte) {
	copy(out[4:20], digestA)
	copy(out[20:20+len(username)], username)
	out[56] = controlCheckStatus
	out[57] = adapterNum
	copy(out[58:64], macXORed)
	copy(out[64:80], digestB)
}

// writeLoginIPRegion writes the IP section marker and reported IPv4 at
// [80,85), MD5-C at [97,105) and IPDog at 105. [106,110) stays zero.
func writeLoginIPRegion(out []byte, clientIPv4 [4]byte, digestC []byte, ipdog byte) {
	out[80] = 0x01
	copy(out[81:85], clientIPv4[:])
	copy(out[97:105], digestC)
	out[105] = ipdog
}

// writeLoginHostRegion writes host_name at [110,110+len), the primary DNS at
// [142,146), DHCP IPv4 at [146,150), secondary DNS at [150,154), os_info at
// [162,182) and host_os at [182,182+len).
func writeLoginHostRegion(out, hostName []byte, primaryDNS, dhcpIPv4, secondaryDNS [4]byte, osInfo [20]byte, hostOS []byte) {
	copy(out[110:110+len(hostName)], hostName)
	copy(out[142:146], primaryDNS[:])
	copy(out[146:150], dhcpIPv4[:])
	copy(out[150:154], secondaryDNS[:])
	copy(out[162:182], osInfo[:])
	copy(out[182:182+len(hostOS)], hostOS)
}

// writeLoginAuthExtension writes auth_version at [310,312), the auth ext
// marker 02 0c at [312,314), the raw MAC at [320,326), the CRC-1968 at
// [314,318) and the auth ext tail at [328,330). The raw MAC must be written
// before the CRC, whose input spans [0,312) and [320,326).
func writeLoginAuthExtension(out []byte, authVersion [2]byte, mac [6]byte, authExtTail [2]byte) {
	copy(out[310:312], authVersion[:])
	out[312] = 0x02
	out[313] = 0x0c
	copy(out[320:326], mac[:])
	copy(out[314:318], crc1968(loginCRCTailInput(out)))
	copy(out[328:330], authExtTail[:])
}

// loginCRCTailInput returns the CRC-1968 input
// login[0:312] || 0x01 0x26 0x07 0x11 0x00 0x00 || login[320:326]. The raw
// MAC at [320,326) must already be written.
func loginCRCTailInput(login []byte) []byte {
	in := make([]byte, 0, 312+6+macLength)
	in = append(in, login[0:312]...)
	in = append(in, 0x01, 0x26, 0x07, 0x11, 0x00, 0x00)
	in = append(in, login[320:326]...)
	return in
}

// loginResponseKind tags a parsed Login response as success or rejection.
type loginResponseKind uint8

const (
	loginResponseSuccess   loginResponseKind = 1
	loginResponseRejection loginResponseKind = 2
)

// loginResponse is the tagged result of parsing a Login response. On success
// it carries the 16-byte Auth Info; on rejection it carries the one-byte wire
// error code. It never retains username or password.
type loginResponse struct {
	kind     loginResponseKind
	authInfo [16]byte
	code     byte
}

// parseLoginResponse parses a Login response, distinguishing the 64-byte
// success response (opcode 0x04) from the 32-byte rejection response (opcode
// 0x05) in one pass, and extracts Auth Info or the wire error code.
func parseLoginResponse(resp []byte) (loginResponse, error) {
	if len(resp) == 0 {
		return loginResponse{}, fmt.Errorf("login response is empty")
	}
	switch resp[0] {
	case 0x04:
		if len(resp) != loginSuccessLength {
			return loginResponse{}, fmt.Errorf("login success response is %d bytes, expected %d", len(resp), loginSuccessLength)
		}
		var authInfo [16]byte
		copy(authInfo[:], resp[23:39])
		return loginResponse{kind: loginResponseSuccess, authInfo: authInfo}, nil
	case 0x05:
		if len(resp) != loginRejectionLength {
			return loginResponse{}, fmt.Errorf("login rejection response is %d bytes, expected %d", len(resp), loginRejectionLength)
		}
		return loginResponse{kind: loginResponseRejection, code: resp[4]}, nil
	default:
		return loginResponse{}, fmt.Errorf("login response opcode 0x%02x, expected 0x04 or 0x05", resp[0])
	}
}

// buildKA1Request builds the documented 42-byte KA1 request. digestA is the
// 16-byte MD5-A computed with the current salt; authInfo is the value refilled
// by the Login success response; timestamp is the BE u16 supplied by the Run.
func buildKA1Request(digestA []byte, authInfo [16]byte, timestamp uint16) ([]byte, error) {
	if len(digestA) != authInfoLength {
		return nil, fmt.Errorf("md5-a digest is %d bytes, expected %d", len(digestA), authInfoLength)
	}
	out := make([]byte, ka1RequestLength)
	out[0] = 0xff
	copy(out[1:17], digestA)
	copy(out[20:36], authInfo[:])
	binary.BigEndian.PutUint16(out[36:38], timestamp)
	// [17,20), [38,42) zero.
	return out, nil
}

// parseKA1Response validates the documented 20-byte KA1 response. It checks
// length and the leading opcode byte.
func parseKA1Response(resp []byte) error {
	if len(resp) != ka1ResponseLength {
		return fmt.Errorf("ka1 response is %d bytes, expected %d", len(resp), ka1ResponseLength)
	}
	if resp[0] != 0x07 {
		return fmt.Errorf("ka1 response opcode 0x%02x, expected 0x07", resp[0])
	}
	return nil
}

// isCompleteKA1Response reports whether datagram is structurally a complete
// 20-byte KA1 response (length and opcode only). It is used by the stale
// receive loop to recognize a late KA1 response without advancing state.
func isCompleteKA1Response(datagram []byte) bool {
	return len(datagram) == ka1ResponseLength && datagram[0] == 0x07
}

// inspectKA2Response reports whether datagram is structurally a complete KA2
// response (length, opcode, fixed bytes and a valid type) and, when it is,
// returns the echoed serial and type. It does not match the serial or type
// against an expectation; the caller uses the returned values to decide
// whether the response is the expected one, an ignorable already-sent one, or
// an impossible future response.
func inspectKA2Response(datagram []byte) (serial, typ byte, ok bool) {
	if len(datagram) != ka2ResponseLength {
		return 0, 0, false
	}
	if datagram[0] != 0x07 {
		return 0, 0, false
	}
	if datagram[2] != 0x28 || datagram[3] != 0x00 || datagram[4] != 0x0b {
		return 0, 0, false
	}
	if datagram[5] != ka2Type1 && datagram[5] != ka2Type3 {
		return 0, 0, false
	}
	return datagram[1], datagram[5], true
}

// buildKA2Request builds the documented 40-byte KA2 request. serial is the
// u8 sequence number; typ is 1 or 3; firstType1 selects the init version
// (0x0f 0x27) for the first Type1 packet and the institution keep-alive
// version otherwise; tail is the value refilled by the previous KA2 response
// (all zero for the first packet); clientIPv4 is reported in the Type3 region.
func buildKA2Request(serial, typ byte, firstType1 bool, keepAliveVersion [2]byte, tail [4]byte, clientIPv4 [4]byte) ([]byte, error) {
	if typ != ka2Type1 && typ != ka2Type3 {
		return nil, fmt.Errorf("ka2 type %d: must be 1 or 3", typ)
	}
	out := make([]byte, ka2RequestLength)
	out[0] = 0x07
	out[1] = serial
	out[2] = 0x28
	out[3] = 0x00
	out[4] = 0x0b
	out[5] = typ
	if firstType1 && typ == ka2Type1 {
		out[6] = ka2InitVersionHi
		out[7] = ka2InitVersionLo
	} else {
		out[6] = keepAliveVersion[0]
		out[7] = keepAliveVersion[1]
	}
	out[8] = 0x2f
	out[9] = 0x12
	copy(out[16:20], tail[:])
	if typ == ka2Type3 {
		copy(out[28:32], clientIPv4[:])
	}
	return out, nil
}

// ka2Response carries the Tail extracted from a 60-byte KA2 response.
type ka2Response struct {
	tail [4]byte
}

// parseKA2Response parses the documented 60-byte KA2 response, validating
// length, opcode and the echoed serial, fixed bytes and type, and extracts
// the four-byte Tail.
func parseKA2Response(resp []byte, expectedSerial, expectedType byte) (ka2Response, error) {
	if len(resp) != ka2ResponseLength {
		return ka2Response{}, fmt.Errorf("ka2 response is %d bytes, expected %d", len(resp), ka2ResponseLength)
	}
	if resp[0] != 0x07 {
		return ka2Response{}, fmt.Errorf("ka2 response opcode 0x%02x, expected 0x07", resp[0])
	}
	if resp[1] != expectedSerial {
		return ka2Response{}, fmt.Errorf("ka2 response serial %d, expected %d", resp[1], expectedSerial)
	}
	if resp[2] != 0x28 || resp[3] != 0x00 || resp[4] != 0x0b {
		return ka2Response{}, fmt.Errorf("ka2 response fixed bytes 0x%02x 0x%02x 0x%02x, expected 28 00 0b", resp[2], resp[3], resp[4])
	}
	if resp[5] != expectedType {
		return ka2Response{}, fmt.Errorf("ka2 response type %d, expected %d", resp[5], expectedType)
	}
	var tail [4]byte
	copy(tail[:], resp[16:20])
	return ka2Response{tail: tail}, nil
}

// buildLogoutRequest builds the exact 80-byte Logout request. It validates and
// encodes only the username and password, so an unusable host field cannot
// prevent best-effort cleanup. It uses the fresh salt supplied by the Run for
// MD5-A and MAC XOR, and the Auth Info refilled by the Login success response.
func buildLogoutRequest(in loginInput, salt [4]byte, authInfo [16]byte) ([]byte, error) {
	username, password, err := encodeLogoutCredentialFields(in)
	if err != nil {
		return nil, err
	}

	digestA := md5A(salt[:], password)
	macXORed, err := macXOR(in.mac, digestA)
	if err != nil {
		return nil, fmt.Errorf("compute mac xor: %w", err)
	}

	out := make([]byte, logoutRequestLength)
	out[0] = 0x06
	out[1] = 0x01
	out[2] = 0x00
	out[3] = byte(20 + len(username))
	copy(out[4:20], digestA)
	copy(out[20:20+len(username)], username)
	out[56] = in.controlCheckStatus
	out[57] = in.adapterNum
	copy(out[58:64], macXORed)
	copy(out[64:80], authInfo[:])
	return out, nil
}

// parseLogoutACK validates the documented four-byte Logout ACK.
func parseLogoutACK(resp []byte) error {
	if len(resp) != logoutACKLength {
		return fmt.Errorf("logout ack is %d bytes, expected %d", len(resp), logoutACKLength)
	}
	if resp[0] != 0x04 || resp[1] != 0x00 || resp[2] != 0x00 || resp[3] != 0x00 {
		return fmt.Errorf("logout ack bytes 0x%02x 0x%02x 0x%02x 0x%02x, expected 04 00 00 00", resp[0], resp[1], resp[2], resp[3])
	}
	return nil
}
