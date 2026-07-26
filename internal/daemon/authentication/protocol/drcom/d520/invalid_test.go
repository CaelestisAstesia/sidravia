package d520

import (
	"bytes"
	"strings"
	"testing"
)

// fictionalSecrets are substrings that must never appear in an error message.
var fictionalSecrets = []string{"student-test", "local-test-password"}

func assertRejectedNoSecrets(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	for _, s := range fictionalSecrets {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error message leaks fictional secret %q: %v", s, err)
		}
	}
}

func validInput(t *testing.T) loginInput {
	t.Helper()
	doc := loadFixture(t)
	return buildLoginInput(t, doc, mustHex2(t, "1234"))
}

// TestRejectsInvalidInputs proves a compact table of representative invalid
// inputs is rejected and that no error message carries a fictional
// username/password marker.
func TestRejectsInvalidInputs(t *testing.T) {
	salt := mustHex4(t, "01020304")
	keepAliveVersion := mustHex2(t, "dc02")
	clientIPv4 := mustIPv4(t, "10.0.0.2")
	var zeroTail [4]byte

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			"username not strict GBK",
			func(t *testing.T) {
				in := validInput(t)
				in.username = "😀"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"password not strict GBK",
			func(t *testing.T) {
				in := validInput(t)
				in.password = "secret😀"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host name not strict GBK",
			func(t *testing.T) {
				in := validInput(t)
				in.hostName = "host😀"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host os not strict GBK",
			func(t *testing.T) {
				in := validInput(t)
				in.hostOS = "OS😀"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"username over 36 bytes",
			func(t *testing.T) {
				in := validInput(t)
				in.username = strings.Repeat("a", 37)
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host name over 32 bytes",
			func(t *testing.T) {
				in := validInput(t)
				in.hostName = strings.Repeat("h", 33)
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host os over 32 bytes",
			func(t *testing.T) {
				in := validInput(t)
				in.hostOS = strings.Repeat("o", 33)
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"logout username not strict GBK",
			func(t *testing.T) {
				in := validInput(t)
				in.username = "😀"
				_, err := buildLogoutRequest(in, salt, mustHex16(t, "05bbfe85e226f735ad5fc3ab4dcc6e76"))
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"username embedded NUL",
			func(t *testing.T) {
				in := validInput(t)
				in.username = "stu\x00dent"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"password embedded NUL",
			func(t *testing.T) {
				in := validInput(t)
				in.password = "sec\x00ret"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host name embedded NUL",
			func(t *testing.T) {
				in := validInput(t)
				in.hostName = "ho\x00st"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host os embedded NUL",
			func(t *testing.T) {
				in := validInput(t)
				in.hostOS = "OS\x00"
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"logout ignores unusable host name",
			func(t *testing.T) {
				in := validInput(t)
				in.hostName = "host😀"
				_, err := buildLogoutRequest(in, salt, mustHex16(t, "05bbfe85e226f735ad5fc3ab4dcc6e76"))
				if err != nil {
					t.Fatalf("logout rejected on unusable host field: %v", err)
				}
			},
		},
		{
			"username fits in runes but exceeds 36 encoded bytes",
			func(t *testing.T) {
				in := validInput(t)
				in.username = strings.Repeat("星", 19) // 38 GBK bytes
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host name fits in runes but exceeds 32 encoded bytes",
			func(t *testing.T) {
				in := validInput(t)
				in.hostName = strings.Repeat("星", 17) // 34 GBK bytes
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"host os fits in runes but exceeds 32 encoded bytes",
			func(t *testing.T) {
				in := validInput(t)
				in.hostOS = strings.Repeat("星", 17) // 34 GBK bytes
				_, err := buildLoginRequest(in, salt)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"username in strict GBK accepted",
			func(t *testing.T) {
				in := validInput(t)
				in.username = "用户"
				if _, err := buildLoginRequest(in, salt); err != nil {
					t.Fatalf("strict GBK username rejected: %v", err)
				}
			},
		},
		{
			"ka2 type 0",
			func(t *testing.T) {
				_, err := buildKA2Request(0, 0, true, keepAliveVersion, zeroTail, clientIPv4)
				assertRejectedNoSecrets(t, err)
			},
		},
		{
			"ka2 type 5",
			func(t *testing.T) {
				_, err := buildKA2Request(0, 5, true, keepAliveVersion, zeroTail, clientIPv4)
				assertRejectedNoSecrets(t, err)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

// TestRejectsMalformedResponses proves malformed responses are rejected by
// structural minimums, the private maximum datagram bound, wrong opcode or
// discriminator and echoed-field mismatches, without leaking secrets.
func TestRejectsMalformedResponses(t *testing.T) {
	doc := loadFixture(t)
	ka2Type1Resp := mustHex(t, exchange(t, doc, "ka2_type1_first").DeclaredResponseHex)

	// oversized returns a max-bound-plus-one datagram carrying the given
	// leading opcode.
	oversized := func(opcode byte) []byte {
		b := make([]byte, 4097)
		b[0] = opcode
		return b
	}

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"challenge response empty", func(t *testing.T) {
			_, err := parseChallengeResponse([]byte{})
			assertRejectedNoSecrets(t, err)
		}},
		{"challenge response below minimum structure", func(t *testing.T) {
			b := make([]byte, 7)
			b[0] = 0x02
			_, err := parseChallengeResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"challenge response wrong opcode", func(t *testing.T) {
			b := make([]byte, 8)
			b[0] = 0x03
			_, err := parseChallengeResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"challenge response oversized", func(t *testing.T) {
			_, err := parseChallengeResponse(oversized(0x02))
			assertRejectedNoSecrets(t, err)
		}},
		{"login response empty", func(t *testing.T) {
			_, err := parseLoginResponse([]byte{})
			assertRejectedNoSecrets(t, err)
		}},
		{"login success response below minimum structure", func(t *testing.T) {
			b := make([]byte, 38)
			b[0] = 0x04
			_, err := parseLoginResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"login rejection response below minimum structure", func(t *testing.T) {
			b := make([]byte, 4)
			b[0] = 0x05
			_, err := parseLoginResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"login response wrong opcode", func(t *testing.T) {
			b := make([]byte, 64)
			b[0] = 0x06
			_, err := parseLoginResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"login response oversized", func(t *testing.T) {
			_, err := parseLoginResponse(oversized(0x04))
			assertRejectedNoSecrets(t, err)
		}},
		{"ka1 response empty", func(t *testing.T) {
			err := parseKA1Response([]byte{})
			assertRejectedNoSecrets(t, err)
		}},
		{"ka1 response wrong opcode", func(t *testing.T) {
			err := parseKA1Response([]byte{0x06})
			assertRejectedNoSecrets(t, err)
		}},
		{"ka1 response oversized", func(t *testing.T) {
			err := parseKA1Response(oversized(0x07))
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response empty", func(t *testing.T) {
			_, err := parseKA2Response([]byte{}, 0x00, ka2Type1, true)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response below minimum structure", func(t *testing.T) {
			_, err := parseKA2Response(ka2Type1Resp[:19], 0x00, ka2Type1, true)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response wrong opcode", func(t *testing.T) {
			b := append([]byte(nil), ka2Type1Resp...)
			b[0] = 0x06
			_, err := parseKA2Response(b, 0x00, ka2Type1, true)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response serial mismatch", func(t *testing.T) {
			_, err := parseKA2Response(ka2Type1Resp, 0x01, ka2Type1, false)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response type incompatible with request", func(t *testing.T) {
			_, err := parseKA2Response(ka2Type1Resp, 0x00, ka2Type3, false)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response unrecognized type", func(t *testing.T) {
			b := append([]byte(nil), ka2Type1Resp...)
			b[5] = 0x05
			_, err := parseKA2Response(b, 0x00, ka2Type1, true)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response fixed bytes wrong", func(t *testing.T) {
			b := append([]byte(nil), ka2Type1Resp...)
			b[2] = 0x00
			_, err := parseKA2Response(b, 0x00, ka2Type1, true)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response oversized", func(t *testing.T) {
			b := oversized(0x07)
			b[2] = 0x28
			b[3] = 0x00
			b[4] = 0x0b
			_, err := parseKA2Response(b, 0x00, ka2Type1, true)
			assertRejectedNoSecrets(t, err)
		}},
		{"logout response empty", func(t *testing.T) {
			err := parseLogoutACK([]byte{})
			assertRejectedNoSecrets(t, err)
		}},
		{"logout response wrong opcode", func(t *testing.T) {
			err := parseLogoutACK([]byte{0x05, 0x00, 0x00, 0x00})
			assertRejectedNoSecrets(t, err)
		}},
		{"logout response oversized", func(t *testing.T) {
			err := parseLogoutACK(oversized(0x04))
			assertRejectedNoSecrets(t, err)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

// TestAliasSafety proves caller-owned byte slices can be mutated after helper
// calls without changing returned parsed values or previously built packets.
func TestAliasSafety(t *testing.T) {
	doc := loadFixture(t)

	// Parser: mutating the response after parsing must not change parsed values.
	challengeResp := mustHex(t, exchange(t, doc, "challenge_for_login").DeclaredResponseHex)
	parsed, err := parseChallengeResponse(challengeResp)
	if err != nil {
		t.Fatalf("parse challenge: %v", err)
	}
	saltBefore := parsed.salt
	challengeResp[4] = 0xff
	if parsed.salt != saltBefore {
		t.Fatalf("parsed salt changed after response mutation")
	}

	loginSuccessResp := mustHex(t, exchange(t, doc, "login_success").DeclaredResponseHex)
	loginParsed, err := parseLoginResponse(loginSuccessResp)
	if err != nil {
		t.Fatalf("parse login success: %v", err)
	}
	authInfoBefore := loginParsed.authInfo
	loginSuccessResp[23] = 0xff
	if loginParsed.authInfo != authInfoBefore {
		t.Fatalf("parsed auth info changed after response mutation")
	}

	ka2Resp := mustHex(t, exchange(t, doc, "ka2_type1_first").DeclaredResponseHex)
	ka2Parsed, err := parseKA2Response(ka2Resp, 0x00, ka2Type1, true)
	if err != nil {
		t.Fatalf("parse ka2: %v", err)
	}
	if ka2Parsed.tailRefill == nil {
		t.Fatal("parsed normal KA2 response has no Tail refill")
	}
	tailBefore := *ka2Parsed.tailRefill
	ka2Resp[16] = 0xff
	if *ka2Parsed.tailRefill != tailBefore {
		t.Fatal("parsed Tail refill changed after response mutation")
	}

	// Builder: mutating the input after building must not change the packet.
	in := buildLoginInput(t, doc, mustHex2(t, "1234"))
	salt := mustHex4(t, "01020304")
	packet, err := buildLoginRequest(in, salt)
	if err != nil {
		t.Fatalf("build login: %v", err)
	}
	packetCopy := append([]byte(nil), packet...)
	in.username = "mutated"
	in.password = "mutated"
	in.mac[0] = 0xff
	salt[0] = 0xff
	if !bytes.Equal(packet, packetCopy) {
		t.Fatalf("built packet changed after input mutation")
	}

	// Crypto: mutating input slices after computing must not change the digest.
	cryptoSalt := []byte{0x01, 0x02, 0x03, 0x04}
	password := []byte(doc.FictionalInputs.Password)
	digestA := md5A(cryptoSalt, password)
	digestACopy := append([]byte(nil), digestA...)
	cryptoSalt[0] = 0xff
	password[0] = 0xff
	if !bytes.Equal(digestA, digestACopy) {
		t.Fatalf("md5-a digest changed after input mutation")
	}

	mac := mustHex6(t, doc.FictionalInputs.MacHex)
	xor, err := macXOR(mac, digestACopy)
	if err != nil {
		t.Fatalf("mac xor: %v", err)
	}
	xorCopy := append([]byte(nil), xor...)
	digestACopy[0] = 0xff
	if !bytes.Equal(xor, xorCopy) {
		t.Fatalf("mac xor changed after input mutation")
	}
}
