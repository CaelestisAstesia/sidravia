package d520

import (
	"bytes"
	"testing"
)

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
