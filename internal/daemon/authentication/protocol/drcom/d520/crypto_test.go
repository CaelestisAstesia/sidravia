package d520

import (
	"bytes"
	"strings"
	"testing"
)

// TestProtocolTextEncodesStrictGBK proves the protocol-text encoder is strict
// GBK: a Chinese string encodes to its documented GBK byte sequence, ASCII
// stays byte-identical, and invalid UTF-8, embedded NUL and runes the GBK
// encoder cannot represent are rejected without echoing the input text.
func TestProtocolTextEncodesStrictGBK(t *testing.T) {
	t.Run("chinese text encodes to expected gbk bytes", func(t *testing.T) {
		got, err := encodeProtocolText("校园终端")
		if err != nil {
			t.Fatalf("encode strict GBK: %v", err)
		}
		want := []byte{0xd0, 0xa3, 0xd4, 0xb0, 0xd6, 0xd5, 0xb6, 0xcb}
		if !bytes.Equal(got, want) {
			t.Fatalf("gbk encoding = %x, want %x", got, want)
		}
	})

	t.Run("ascii remains byte identical", func(t *testing.T) {
		got, err := encodeProtocolText("student-test")
		if err != nil {
			t.Fatalf("encode ascii: %v", err)
		}
		if !bytes.Equal(got, []byte("student-test")) {
			t.Fatalf("ascii encoding = %x, want byte-identical input", got)
		}
	})

	t.Run("unrepresentable rune rejected without echoing input", func(t *testing.T) {
		_, err := encodeProtocolText("ze😀ro")
		if err == nil {
			t.Fatal("expected error for a rune GBK cannot represent, got nil")
		}
		if strings.Contains(err.Error(), "😀") || strings.Contains(err.Error(), "ze") {
			t.Fatalf("error echoes input text: %v", err)
		}
	})

	t.Run("invalid utf-8 rejected", func(t *testing.T) {
		if _, err := encodeProtocolText(string([]byte{0xff, 0xfe, 0xfd})); err == nil {
			t.Fatal("expected error for invalid UTF-8, got nil")
		}
	})

	t.Run("embedded NUL rejected", func(t *testing.T) {
		if _, err := encodeProtocolText("ab\x00cd"); err == nil {
			t.Fatal("expected error for embedded NUL, got nil")
		}
	})

	t.Run("each call returns a fresh slice", func(t *testing.T) {
		first, err := encodeProtocolText("校园终端")
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		first[0] ^= 0xff
		second, err := encodeProtocolText("校园终端")
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if !bytes.Equal(second, []byte{0xd0, 0xa3, 0xd4, 0xb0, 0xd6, 0xd5, 0xb6, 0xcb}) {
			t.Fatalf("second encoding = %x, want fictional GBK vector (first result mutation leaked)", second)
		}
	})
}

// TestCryptoMD5A proves MD5-A = MD5(0x03 0x01 || salt || passwordGBK) equals
// the fixture intermediate, and that the composed input matches the fixture
// recorded input byte-for-byte.
func TestCryptoMD5A(t *testing.T) {
	doc := loadFixture(t)
	salt := mustHex4(t, doc.CryptoIntermediates.SaltHex)
	fi := doc.FictionalInputs
	composed := []byte{0x03, 0x01}
	composed = append(composed, salt[:]...)
	composed = append(composed, []byte(fi.Password)...)
	if !bytes.Equal(composed, mustHex(t, doc.CryptoIntermediates.MD5A.InputHex)) {
		t.Fatalf("md5-a input composition mismatch")
	}
	got := md5A(salt[:], []byte(fi.Password))
	want := mustHex(t, doc.CryptoIntermediates.MD5A.OutputHex)
	if !bytes.Equal(got, want) {
		t.Fatalf("md5-a = %x, want %x", got, want)
	}
}

// TestCryptoMD5B proves MD5-B = MD5(0x01 || password || salt || 0x00*4).
func TestCryptoMD5B(t *testing.T) {
	doc := loadFixture(t)
	salt := mustHex4(t, doc.CryptoIntermediates.SaltHex)
	fi := doc.FictionalInputs
	composed := []byte{0x01}
	composed = append(composed, []byte(fi.Password)...)
	composed = append(composed, salt[:]...)
	composed = append(composed, 0x00, 0x00, 0x00, 0x00)
	if !bytes.Equal(composed, mustHex(t, doc.CryptoIntermediates.MD5B.InputHex)) {
		t.Fatalf("md5-b input composition mismatch")
	}
	got := md5B(salt[:], []byte(fi.Password))
	want := mustHex(t, doc.CryptoIntermediates.MD5B.OutputHex)
	if !bytes.Equal(got, want) {
		t.Fatalf("md5-b = %x, want %x", got, want)
	}
}

// TestCryptoMD5C proves MD5-C = MD5(0x01 || clientIPv4 || 0x00*12 || 0x14 0x00 0x07 0x0b)[:8].
func TestCryptoMD5C(t *testing.T) {
	doc := loadFixture(t)
	clientIPv4 := mustIPv4(t, doc.FictionalInputs.ClientIPv4)
	got := md5C(clientIPv4)
	want := mustHex(t, doc.CryptoIntermediates.MD5C.OutputHex)
	if len(want) != 8 {
		t.Fatalf("md5-c fixture output is %d bytes, expected 8", len(want))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("md5-c = %x, want %x", got, want)
	}
}

// TestCryptoMACXOR proves MAC XOR = mac[i] ^ MD5-A[i] for i in 0..6.
func TestCryptoMACXOR(t *testing.T) {
	doc := loadFixture(t)
	mac := mustHex6(t, doc.FictionalInputs.MacHex)
	digestA := mustHex(t, doc.CryptoIntermediates.MD5A.OutputHex)
	got, err := macXOR(mac, digestA)
	if err != nil {
		t.Fatalf("mac xor: %v", err)
	}
	want := mustHex(t, doc.CryptoIntermediates.MACXOR.OutputHex)
	if !bytes.Equal(got, want) {
		t.Fatalf("mac xor = %x, want %x", got, want)
	}
}

// TestCryptoCRC1968 proves the CRC-1968 algorithm against the documented
// self-contained vector checksum_d_series(010203040506) == 206d16d7, and
// against the fixture login CRC input.
func TestCryptoCRC1968(t *testing.T) {
	got := crc1968(mustHex(t, "010203040506"))
	want := mustHex(t, "206d16d7")
	if !bytes.Equal(got, want) {
		t.Fatalf("crc1968(010203040506) = %x, want %x", got, want)
	}

	doc := loadFixture(t)
	gotLogin := crc1968(mustHex(t, doc.CryptoIntermediates.CRC1968.InputHex))
	wantLogin := mustHex(t, doc.CryptoIntermediates.CRC1968.OutputHex)
	if !bytes.Equal(gotLogin, wantLogin) {
		t.Fatalf("crc1968(login input) = %x, want %x", gotLogin, wantLogin)
	}
}
