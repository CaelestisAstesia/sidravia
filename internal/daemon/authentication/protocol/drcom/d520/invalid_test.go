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
// exact length, opcode and echoed fields, without leaking secrets.
func TestRejectsMalformedResponses(t *testing.T) {
	doc := loadFixture(t)
	challengeResp := mustHex(t, exchange(t, doc, "challenge_for_login").DeclaredResponseHex)
	loginSuccessResp := mustHex(t, exchange(t, doc, "login_success").DeclaredResponseHex)
	loginRejectionResp := mustHex(t, doc.Rejection.Login.DeclaredResponseHex)
	ka1Resp := mustHex(t, exchange(t, doc, "ka1").DeclaredResponseHex)
	ka2Type1Resp := mustHex(t, exchange(t, doc, "ka2_type1_first").DeclaredResponseHex)
	logoutACK := mustHex(t, exchange(t, doc, "logout").DeclaredResponseHex)

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"challenge response too short", func(t *testing.T) {
			_, err := parseChallengeResponse(challengeResp[:15])
			assertRejectedNoSecrets(t, err)
		}},
		{"challenge response wrong opcode", func(t *testing.T) {
			b := append([]byte(nil), challengeResp...)
			b[0] = 0x03
			_, err := parseChallengeResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"login success response too short", func(t *testing.T) {
			_, err := parseLoginResponse(loginSuccessResp[:63])
			assertRejectedNoSecrets(t, err)
		}},
		{"login success response wrong opcode", func(t *testing.T) {
			b := append([]byte(nil), loginSuccessResp...)
			b[0] = 0x05
			_, err := parseLoginResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"login rejection response too short", func(t *testing.T) {
			_, err := parseLoginResponse(loginRejectionResp[:31])
			assertRejectedNoSecrets(t, err)
		}},
		{"login rejection response wrong opcode", func(t *testing.T) {
			b := append([]byte(nil), loginRejectionResp...)
			b[0] = 0x04
			_, err := parseLoginResponse(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka1 response too short", func(t *testing.T) {
			err := parseKA1Response(ka1Resp[:19])
			assertRejectedNoSecrets(t, err)
		}},
		{"ka1 response wrong opcode", func(t *testing.T) {
			b := append([]byte(nil), ka1Resp...)
			b[0] = 0x06
			err := parseKA1Response(b)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response too short", func(t *testing.T) {
			_, err := parseKA2Response(ka2Type1Resp[:59], 0x00, ka2Type1)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response wrong opcode", func(t *testing.T) {
			b := append([]byte(nil), ka2Type1Resp...)
			b[0] = 0x06
			_, err := parseKA2Response(b, 0x00, ka2Type1)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response serial mismatch", func(t *testing.T) {
			_, err := parseKA2Response(ka2Type1Resp, 0x01, ka2Type1)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response type mismatch", func(t *testing.T) {
			_, err := parseKA2Response(ka2Type1Resp, 0x00, ka2Type3)
			assertRejectedNoSecrets(t, err)
		}},
		{"ka2 response fixed bytes wrong", func(t *testing.T) {
			b := append([]byte(nil), ka2Type1Resp...)
			b[2] = 0x00
			_, err := parseKA2Response(b, 0x00, ka2Type1)
			assertRejectedNoSecrets(t, err)
		}},
		{"logout ack too short", func(t *testing.T) {
			err := parseLogoutACK(logoutACK[:3])
			assertRejectedNoSecrets(t, err)
		}},
		{"logout ack wrong bytes", func(t *testing.T) {
			b := append([]byte(nil), logoutACK...)
			b[0] = 0x05
			err := parseLogoutACK(b)
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
	ka2Parsed, err := parseKA2Response(ka2Resp, 0x00, ka2Type1)
	if err != nil {
		t.Fatalf("parse ka2: %v", err)
	}
	tailBefore := ka2Parsed.tail
	ka2Resp[16] = 0xff
	if ka2Parsed.tail != tailBefore {
		t.Fatalf("parsed tail changed after response mutation")
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
