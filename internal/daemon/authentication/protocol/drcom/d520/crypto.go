package d520

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// encodeProtocolText encodes s as the protocol text byte sequence. The current
// implementation uses strict GBK. An embedded NUL
// (0x00) is rejected because these fields are NUL-padded on the wire and an
// embedded NUL would be indistinguishable from padding. The returned slice is
// freshly allocated and never aliases s.
func encodeProtocolText(s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("not valid UTF-8, cannot encode as protocol text")
	}
	for _, r := range s {
		if r == 0 {
			return nil, fmt.Errorf("contains an embedded NUL, cannot encode as protocol text")
		}
	}
	encoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(s))
	if err != nil {
		return nil, fmt.Errorf("contains a rune that cannot be encoded as strict GBK")
	}
	return append([]byte(nil), encoded...), nil
}

// md5A = MD5(0x03 0x01 || salt || passwordGBK), 16 bytes. Used by Login, KA1
// and Logout, each with the salt that is current for that step.
func md5A(salt []byte, passwordGBK []byte) []byte {
	h := md5.New()
	h.Write([]byte{0x03, 0x01})
	h.Write(salt)
	h.Write(passwordGBK)
	return h.Sum(nil)
}

// md5B = MD5(0x01 || passwordGBK || salt || 0x00 0x00 0x00 0x00), 16 bytes.
// Used only by Login.
func md5B(salt []byte, passwordGBK []byte) []byte {
	h := md5.New()
	h.Write([]byte{0x01})
	h.Write(passwordGBK)
	h.Write(salt)
	h.Write([]byte{0x00, 0x00, 0x00, 0x00})
	return h.Sum(nil)
}

// md5C = MD5(ipSection || 0x14 0x00 0x07 0x0b)[:8], where
// ipSection = 0x01 || clientIPv4 || 0x00*12 (17 bytes). Used only by Login.
func md5C(clientIPv4 [4]byte) []byte {
	ipSection := make([]byte, 17)
	ipSection[0] = 0x01
	copy(ipSection[1:5], clientIPv4[:])
	h := md5.New()
	h.Write(ipSection)
	h.Write([]byte{0x14, 0x00, 0x07, 0x0b})
	return h.Sum(nil)[:8]
}

// macXOR = mac[i] ^ md5ADigest[i] for i in 0..6, 6 bytes. The server XORs the
// same way to recover the MAC and requires the raw MAC to reappear in the
// Login packet.
func macXOR(mac [6]byte, md5ADigest []byte) ([]byte, error) {
	if len(md5ADigest) < macLength {
		return nil, fmt.Errorf("md5-a digest is %d bytes, need at least %d", len(md5ADigest), macLength)
	}
	out := make([]byte, macLength)
	for i := 0; i < macLength; i++ {
		out[i] = mac[i] ^ md5ADigest[i]
	}
	return out, nil
}

// crc1968 is the D-series checksum. Input is grouped into 4-byte little-endian
// words (the final short group is right-padded with zeros); each word is XORed
// into an initial value of 1234; the result is then multiplied by 1968 modulo
// 2^32 and emitted as 4 little-endian bytes.
func crc1968(input []byte) []byte {
	ret := uint32(1234)
	var group [4]byte
	for i := 0; i < len(input); i += 4 {
		group = [4]byte{}
		end := i + 4
		if end > len(input) {
			end = len(input)
		}
		copy(group[:], input[i:end])
		ret ^= binary.LittleEndian.Uint32(group[:])
	}
	ret *= 1968
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, ret)
	return out
}
