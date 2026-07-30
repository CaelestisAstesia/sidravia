package d520

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestChallengeRequestBuild proves the 20-byte Challenge request is built from
// the caller-supplied seed with all-zero 15-byte padding, for both the login
// and logout challenges.
func TestChallengeRequestBuild(t *testing.T) {
	doc := loadFixture(t)
	for _, name := range []string{"challenge_for_login", "challenge_for_logout"} {
		ex := exchange(t, doc, name)
		req := mustHex(t, ex.RequestHex)
		if len(req) != challengeRequestLength {
			t.Fatalf("%s: request is %d bytes, expected %d", name, len(req), challengeRequestLength)
		}
		seed := binary.LittleEndian.Uint16(req[2:4])
		got := buildChallengeRequest(seed, [15]byte{})
		if !bytes.Equal(got, req) {
			t.Fatalf("%s: buildChallengeRequest = %x, want %x", name, got, req)
		}
	}
}

// TestChallengeResponseParse proves the four-byte salt is extracted from the
// 16-byte Challenge response for both salts in the fixture sequence.
func TestChallengeResponseParse(t *testing.T) {
	doc := loadFixture(t)
	cases := []struct {
		name    string
		saltHex string
	}{
		{"challenge_for_login", "01020304"},
		{"challenge_for_logout", "05060708"},
	}
	for _, tc := range cases {
		ex := exchange(t, doc, tc.name)
		resp := mustHex(t, ex.DeclaredResponseHex)
		got, err := parseChallengeResponse(resp)
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		want := mustHex4(t, tc.saltHex)
		if got.salt != want {
			t.Fatalf("%s: salt = %x, want %x", tc.name, got.salt, want)
		}
	}
}

// TestLoginRequestBuild proves the 330-byte Login request is built byte-exact
// from the immutable input, the login salt and the caller-supplied auth
// extension tail.
func TestLoginRequestBuild(t *testing.T) {
	doc := loadFixture(t)
	loginEx := exchange(t, doc, "login_success")
	want := mustHex(t, loginEx.RequestHex)
	if len(want) != loginRequestLength {
		t.Fatalf("login request is %d bytes, expected %d", len(want), loginRequestLength)
	}
	var authExtTail [2]byte
	copy(authExtTail[:], want[328:330])

	challengeEx := exchange(t, doc, "challenge_for_login")
	salt, err := parseChallengeResponse(mustHex(t, challengeEx.DeclaredResponseHex))
	if err != nil {
		t.Fatalf("parse login challenge: %v", err)
	}

	in := buildLoginInput(t, doc, authExtTail)
	got, err := buildLoginRequest(in, salt.salt)
	if err != nil {
		t.Fatalf("buildLoginRequest: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildLoginRequest mismatch:\n got %x\n want %x", got, want)
	}
}

// TestLoginRequestNonZeroPaddingAppearsOnlyAtThreeRanges proves nonzero Login
// padding values are written only at [106,110), [154,162) and [326,328). The
// only other bytes that differ from the all-zero-padding request are the
// recomputed CRC-1968 at [314,318): its input spans [0,312) and therefore
// includes the IPDog and DHCP padding, so it must change and must be correct.
// Login length, header, username, hashes, MAC XOR/raw MAC, DNS/DHCP/IP/host/OS
// fields, the auth-extension tail and the random tail are unchanged.
func TestLoginRequestNonZeroPaddingAppearsOnlyAtThreeRanges(t *testing.T) {
	doc := loadFixture(t)
	challengeEx := exchange(t, doc, "challenge_for_login")
	salt, err := parseChallengeResponse(mustHex(t, challengeEx.DeclaredResponseHex))
	if err != nil {
		t.Fatalf("parse login challenge: %v", err)
	}
	tail := mustHex2(t, "1234")

	zeroIn := buildLoginInput(t, doc, tail)
	zeroReq, err := buildLoginRequest(zeroIn, salt.salt)
	if err != nil {
		t.Fatalf("build zero-padding login: %v", err)
	}
	if len(zeroReq) != loginRequestLength {
		t.Fatalf("zero-padding login length = %d, want %d", len(zeroReq), loginRequestLength)
	}

	nonzeroIn := zeroIn
	nonzeroIn.loginIPDogPadding = [4]byte{0xde, 0xad, 0xbe, 0xef}
	nonzeroIn.loginDHCPPadding = [8]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	nonzeroIn.loginAuthExtensionPadding = [2]byte{0xfe, 0xed}
	nonzeroReq, err := buildLoginRequest(nonzeroIn, salt.salt)
	if err != nil {
		t.Fatalf("build nonzero-padding login: %v", err)
	}
	if len(nonzeroReq) != loginRequestLength {
		t.Fatalf("nonzero-padding login length = %d, want %d", len(nonzeroReq), loginRequestLength)
	}

	// The three padding ranges carry the nonzero values exactly.
	if !bytes.Equal(nonzeroReq[106:110], nonzeroIn.loginIPDogPadding[:]) {
		t.Fatalf("loginIPDogPadding range = %x, want %x", nonzeroReq[106:110], nonzeroIn.loginIPDogPadding)
	}
	if !bytes.Equal(nonzeroReq[154:162], nonzeroIn.loginDHCPPadding[:]) {
		t.Fatalf("loginDHCPPadding range = %x, want %x", nonzeroReq[154:162], nonzeroIn.loginDHCPPadding)
	}
	if !bytes.Equal(nonzeroReq[326:328], nonzeroIn.loginAuthExtensionPadding[:]) {
		t.Fatalf("loginAuthExtensionPadding range = %x, want %x", nonzeroReq[326:328], nonzeroIn.loginAuthExtensionPadding)
	}

	// Every differing position must lie within the three padding ranges or the
	// recomputed CRC range [314,318); padding must not leak anywhere else.
	allowed := make(map[int]bool)
	for _, r := range [][2]int{{106, 110}, {154, 162}, {314, 318}, {326, 328}} {
		for i := r[0]; i < r[1]; i++ {
			allowed[i] = true
		}
	}
	var leaked []int
	for i := 0; i < loginRequestLength; i++ {
		if zeroReq[i] != nonzeroReq[i] && !allowed[i] {
			leaked = append(leaked, i)
		}
	}
	if len(leaked) > 0 {
		t.Fatalf("padding leaked outside the three ranges and CRC: positions %v", leaked)
	}

	// The three padding ranges all differ from the zero-padding request.
	for _, r := range [][2]int{{106, 110}, {154, 162}, {326, 328}} {
		if bytes.Equal(zeroReq[r[0]:r[1]], nonzeroReq[r[0]:r[1]]) {
			t.Fatalf("padding range [%d,%d) did not differ from zero-padding", r[0], r[1])
		}
	}

	// The CRC at [314,318) is the correct CRC-1968 for the nonzero request and
	// differs from the zero-padding CRC, proving it was recomputed from input
	// that includes the IPDog and DHCP padding.
	wantCRC := crc1968(loginCRCTailInput(nonzeroReq))
	if !bytes.Equal(nonzeroReq[314:318], wantCRC) {
		t.Fatalf("nonzero login CRC = %x, want recomputed %x", nonzeroReq[314:318], wantCRC)
	}
	if bytes.Equal(nonzeroReq[314:318], zeroReq[314:318]) {
		t.Fatal("CRC did not change despite nonzero CRC-input padding")
	}
}

// TestLoginResponseParseSuccess proves the 16-byte Auth Info is extracted from
// the 64-byte success response and matches the fixture-derived value.
func TestLoginResponseParseSuccess(t *testing.T) {
	doc := loadFixture(t)
	ex := exchange(t, doc, "login_success")
	resp := mustHex(t, ex.DeclaredResponseHex)
	got, err := parseLoginResponse(resp)
	if err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	if got.kind != loginResponseSuccess {
		t.Fatalf("login response kind = %d, want success", got.kind)
	}
	want := mustHex16(t, doc.CryptoIntermediates.AuthInfo.OutputHex)
	if got.authInfo != want {
		t.Fatalf("auth info = %x, want %x", got.authInfo, want)
	}
}

// TestLoginResponseParseRejection proves the one-byte wire error code is
// extracted from the 32-byte rejection response.
func TestLoginResponseParseRejection(t *testing.T) {
	doc := loadFixture(t)
	resp := mustHex(t, doc.Rejection.Login.DeclaredResponseHex)
	got, err := parseLoginResponse(resp)
	if err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	if got.kind != loginResponseRejection {
		t.Fatalf("login response kind = %d, want rejection", got.kind)
	}
	if got.code != byte(doc.Rejection.Login.WireErrorCode) {
		t.Fatalf("rejection code = %d, want %d", got.code, doc.Rejection.Login.WireErrorCode)
	}
}

// TestKA1RequestBuild proves the 42-byte KA1 request is built byte-exact from
// MD5-A, Auth Info and the caller-supplied BE timestamp.
func TestKA1RequestBuild(t *testing.T) {
	doc := loadFixture(t)
	ex := exchange(t, doc, "ka1")
	want := mustHex(t, ex.RequestHex)
	if len(want) != ka1RequestLength {
		t.Fatalf("ka1 request is %d bytes, expected %d", len(want), ka1RequestLength)
	}
	salt, err := parseChallengeResponse(mustHex(t, exchange(t, doc, "challenge_for_login").DeclaredResponseHex))
	if err != nil {
		t.Fatalf("parse challenge: %v", err)
	}
	authInfo, err := parseLoginResponse(mustHex(t, exchange(t, doc, "login_success").DeclaredResponseHex))
	if err != nil {
		t.Fatalf("parse login success: %v", err)
	}
	digestA := md5A(salt.salt[:], []byte(doc.FictionalInputs.Password))
	timestamp := binary.BigEndian.Uint16(want[36:38])
	got, err := buildKA1Request(digestA, authInfo.authInfo, timestamp)
	if err != nil {
		t.Fatalf("buildKA1Request: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildKA1Request mismatch:\n got %x\n want %x", got, want)
	}
}

// TestKA1ResponseParse proves the 20-byte KA1 response is accepted.
func TestKA1ResponseParse(t *testing.T) {
	doc := loadFixture(t)
	ex := exchange(t, doc, "ka1")
	resp := mustHex(t, ex.DeclaredResponseHex)
	if err := parseKA1Response(resp); err != nil {
		t.Fatalf("parse ka1 response: %v", err)
	}
}

// TestKA2Type1FirstBuild proves the first Type1 KA2 request uses the init
// version 0x0f 0x27 and a zero Tail.
func TestKA2Type1FirstBuild(t *testing.T) {
	doc := loadFixture(t)
	ex := exchange(t, doc, "ka2_type1_first")
	want := mustHex(t, ex.RequestHex)
	if len(want) != ka2RequestLength {
		t.Fatalf("ka2 type1 request is %d bytes, expected %d", len(want), ka2RequestLength)
	}
	keepAliveVersion := mustHex2(t, doc.FictionalInputs.KeepAliveVersionHex)
	clientIPv4 := mustIPv4(t, doc.FictionalInputs.ClientIPv4)
	var tail [4]byte
	copy(tail[:], want[16:20])
	got, err := buildKA2Request(want[1], want[5], true, keepAliveVersion, tail, clientIPv4)
	if err != nil {
		t.Fatalf("buildKA2Request type1: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildKA2Request type1 mismatch:\n got %x\n want %x", got, want)
	}
}

// TestKA2Type3Build proves the Type3 KA2 request uses the keep-alive version,
// the previous response's Tail and the reported client IPv4.
func TestKA2Type3Build(t *testing.T) {
	doc := loadFixture(t)
	ex := exchange(t, doc, "ka2_type3")
	want := mustHex(t, ex.RequestHex)
	if len(want) != ka2RequestLength {
		t.Fatalf("ka2 type3 request is %d bytes, expected %d", len(want), ka2RequestLength)
	}
	keepAliveVersion := mustHex2(t, doc.FictionalInputs.KeepAliveVersionHex)
	clientIPv4 := mustIPv4(t, doc.FictionalInputs.ClientIPv4)
	var tail [4]byte
	copy(tail[:], want[16:20])
	got, err := buildKA2Request(want[1], want[5], false, keepAliveVersion, tail, clientIPv4)
	if err != nil {
		t.Fatalf("buildKA2Request type3: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildKA2Request type3 mismatch:\n got %x\n want %x", got, want)
	}
}

// TestKA2ResponseParse proves the Tail is extracted from each 60-byte KA2
// response and matches the fixture-derived HMAC values.
func TestKA2ResponseParse(t *testing.T) {
	doc := loadFixture(t)
	type1Resp := mustHex(t, exchange(t, doc, "ka2_type1_first").DeclaredResponseHex)
	got1, err := parseKA2Response(type1Resp, 0x00, ka2Type1, true)
	if err != nil {
		t.Fatalf("parse ka2 type1 response: %v", err)
	}
	want1 := mustHex4(t, doc.CryptoIntermediates.KA2TailType1.OutputHex)
	if got1.tailRefill == nil || *got1.tailRefill != want1 {
		t.Fatalf("ka2 type1 tail refill = %v, want %x", got1.tailRefill, want1)
	}

	type3Resp := mustHex(t, exchange(t, doc, "ka2_type3").DeclaredResponseHex)
	got3, err := parseKA2Response(type3Resp, 0x01, ka2Type3, false)
	if err != nil {
		t.Fatalf("parse ka2 type3 response: %v", err)
	}
	want3 := mustHex4(t, doc.CryptoIntermediates.KA2TailType3.OutputHex)
	if got3.tailRefill == nil || *got3.tailRefill != want3 {
		t.Fatalf("ka2 type3 tail refill = %v, want %x", got3.tailRefill, want3)
	}
}

// TestLogoutRequestBuild proves the 80-byte Logout request is built byte-exact
// from the immutable input, the fresh logout salt and the Login Auth Info.
func TestLogoutRequestBuild(t *testing.T) {
	doc := loadFixture(t)
	logoutEx := exchange(t, doc, "logout")
	want := mustHex(t, logoutEx.RequestHex)
	if len(want) != logoutRequestLength {
		t.Fatalf("logout request is %d bytes, expected %d", len(want), logoutRequestLength)
	}
	challengeEx := exchange(t, doc, "challenge_for_logout")
	salt, err := parseChallengeResponse(mustHex(t, challengeEx.DeclaredResponseHex))
	if err != nil {
		t.Fatalf("parse logout challenge: %v", err)
	}
	authInfo, err := parseLoginResponse(mustHex(t, exchange(t, doc, "login_success").DeclaredResponseHex))
	if err != nil {
		t.Fatalf("parse login success: %v", err)
	}
	// authExtTail is not part of the Logout request, but loginInput requires it.
	in := buildLoginInput(t, doc, mustHex2(t, "1234"))
	got, err := buildLogoutRequest(in, salt.salt, authInfo.authInfo)
	if err != nil {
		t.Fatalf("buildLogoutRequest: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("buildLogoutRequest mismatch:\n got %x\n want %x", got, want)
	}
}

// TestLogoutACKParse proves the four-byte Logout ACK is accepted.
func TestLogoutACKParse(t *testing.T) {
	doc := loadFixture(t)
	ex := exchange(t, doc, "logout")
	resp := mustHex(t, ex.DeclaredResponseHex)
	if err := parseLogoutACK(resp); err != nil {
		t.Fatalf("parse logout ack: %v", err)
	}
}

func TestParseKA2BootstrapType6UsesSpecialFrameWithoutTailUpdate(t *testing.T) {
	resp := syntheticResponse(64)
	resp[0] = 0x07
	resp[1] = 0x00
	resp[2] = 0x10
	resp[3] = 0x01
	resp[4] = 0x0b
	resp[5] = 0x06

	got, err := parseKA2Response(resp, 0x00, ka2Type1, true)
	if err != nil {
		t.Fatalf("parse bootstrap Type 6 response: %v", err)
	}
	if got.tailRefill != nil {
		t.Fatalf("bootstrap Type 6 exposed Tail refill %x, want none", *got.tailRefill)
	}
}

// TestResponseParsersAcceptStructuralVariants proves every response parser
// recognizes its phase by structural minimums instead of one historical exact
// length: the sanitized synthetic variants of the observed campus response
// shapes parse, extension bytes stay opaque, and mutating or appending an
// extension never aliases parsed state. Observed lengths (Challenge 76, Login
// success 45, KA1 72, initial KA2 type 6 at 272, KA2 types 2/4 at 40, Logout
// 25) are examples, not a whitelist.
func TestResponseParsersAcceptStructuralVariants(t *testing.T) {
	salt := [4]byte{0x11, 0x22, 0x33, 0x44}
	authInfo := [16]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf}
	tail := [4]byte{0x51, 0x52, 0x53, 0x54}

	t.Run("challenge minimum structure and 76 byte variant", func(t *testing.T) {
		minimal := peerChallengeResponseVariant(salt, 8)
		got, err := parseChallengeResponse(minimal)
		if err != nil {
			t.Fatalf("parse 8-byte challenge response: %v", err)
		}
		if got.salt != salt {
			t.Fatalf("salt = %x, want %x", got.salt, salt)
		}

		variant := peerChallengeResponseVariant(salt, 76)
		parsed, err := parseChallengeResponse(variant)
		if err != nil {
			t.Fatalf("parse 76-byte challenge response: %v", err)
		}
		if parsed.salt != salt {
			t.Fatalf("salt = %x, want %x", parsed.salt, salt)
		}
		// Mutating extension bytes after parsing must not change parsed state.
		variant[40] = 0x00
		if parsed.salt != salt {
			t.Fatalf("parsed salt changed after extension mutation")
		}
		// Appending a further extension must not alias parsed state either.
		extended := append(variant, 0x01, 0x02, 0x03)
		reparsed, err := parseChallengeResponse(extended)
		if err != nil {
			t.Fatalf("parse extended challenge response: %v", err)
		}
		if reparsed.salt != salt || parsed.salt != salt {
			t.Fatalf("appending an extension aliased parsed state")
		}
	})

	t.Run("login success minimum structure and 45 byte variant", func(t *testing.T) {
		minimal := peerLoginSuccessVariant(authInfo, 39)
		got, err := parseLoginResponse(minimal)
		if err != nil {
			t.Fatalf("parse 39-byte login success: %v", err)
		}
		if got.kind != loginResponseSuccess || got.authInfo != authInfo {
			t.Fatalf("kind = %d auth info = %x, want success with %x", got.kind, got.authInfo, authInfo)
		}

		variant := peerLoginSuccessVariant(authInfo, 45)
		parsed, err := parseLoginResponse(variant)
		if err != nil {
			t.Fatalf("parse 45-byte login success: %v", err)
		}
		if parsed.kind != loginResponseSuccess || parsed.authInfo != authInfo {
			t.Fatalf("kind = %d auth info = %x, want success with %x", parsed.kind, parsed.authInfo, authInfo)
		}
		variant[44] = 0x00
		if parsed.authInfo != authInfo {
			t.Fatalf("parsed auth info changed after extension mutation")
		}
	})

	t.Run("login rejection minimum structure", func(t *testing.T) {
		resp := syntheticResponse(5)
		resp[0] = 0x05
		resp[4] = 0x03
		got, err := parseLoginResponse(resp)
		if err != nil {
			t.Fatalf("parse 5-byte login rejection: %v", err)
		}
		if got.kind != loginResponseRejection || got.code != 0x03 {
			t.Fatalf("kind = %d code = %d, want rejection code 3", got.kind, got.code)
		}
	})

	t.Run("ka1 minimum structure and 72 byte variant", func(t *testing.T) {
		if err := parseKA1Response([]byte{0x07}); err != nil {
			t.Fatalf("parse 1-byte ka1 response: %v", err)
		}
		if err := parseKA1Response(peerKA1ResponseVariant(72)); err != nil {
			t.Fatalf("parse 72-byte ka1 response: %v", err)
		}
	})

	t.Run("ka2 minimum structure and observed type variants", func(t *testing.T) {
		minimal := peerKA2ResponseVariant(0x09, ka2Type1, tail, 20)
		got, err := parseKA2Response(minimal, 0x09, ka2Type1, false)
		if err != nil {
			t.Fatalf("parse 20-byte ka2 response: %v", err)
		}
		if got.tailRefill == nil || *got.tailRefill != tail {
			t.Fatalf("tail refill = %v, want %x", got.tailRefill, tail)
		}

		// Initial bootstrap Type1 accepts a special-frame Type 6 without a
		// Tail refill, even when the bounded response has extensions.
		initial := peerKA2BootstrapType6Response(0x00, 272)
		parsedInitial, err := parseKA2Response(initial, 0x00, ka2Type1, true)
		if err != nil {
			t.Fatalf("parse 272-byte type 6 ka2 response: %v", err)
		}
		if parsedInitial.tailRefill != nil {
			t.Fatalf("bootstrap Type 6 tail refill = %x, want none", *parsedInitial.tailRefill)
		}
		initial[100] = 0x00
		if parsedInitial.tailRefill != nil {
			t.Fatal("extension mutation fabricated a Tail refill")
		}

		// Later Type1 request accepts response type 2 (40 bytes).
		later := peerKA2ResponseVariant(0x01, 2, tail, 40)
		parsedLater, err := parseKA2Response(later, 0x01, ka2Type1, false)
		if err != nil {
			t.Fatalf("parse 40-byte type 2 ka2 response: %v", err)
		}
		if parsedLater.tailRefill == nil || *parsedLater.tailRefill != tail {
			t.Fatalf("tail refill = %v, want %x", parsedLater.tailRefill, tail)
		}
		later[16] ^= 0xff
		if *parsedLater.tailRefill != tail {
			t.Fatal("normal response mutation changed parsed Tail refill")
		}

		// Type3 request accepts response type 4 (40 bytes).
		type4 := peerKA2ResponseVariant(0x02, 4, tail, 40)
		parsedType4, err := parseKA2Response(type4, 0x02, ka2Type3, false)
		if _, err := parseKA2Response(peerKA2ResponseVariant(0x00, 6, tail, 40), 0x00, ka2Type1, true); err == nil {
			t.Fatal("bootstrap Type1 accepted normal-framed Type 6")
		}
		if _, err := parseKA2Response(peerKA2BootstrapType6Response(0x00, 40), 0x00, ka2Type1, false); err == nil {
			t.Fatal("later serial-zero Type1 accepted special Type 6")
		}
		if _, err := parseKA2Response(peerKA2BootstrapType6Response(0x02, 40), 0x02, ka2Type3, false); err == nil {
			t.Fatal("Type3 accepted special Type 6")
		}
		specialType2 := peerKA2BootstrapType6Response(0x00, 40)
		specialType2[5] = 2
		if _, err := parseKA2Response(specialType2, 0x00, ka2Type1, true); err == nil {
			t.Fatal("bootstrap Type1 accepted non-Type-6 special frame")
		}
		if _, err := parseKA2Response(peerKA2ResponseVariant(0x02, 1, tail, 40), 0x02, ka2Type3, false); err == nil {
			t.Fatal("Type3 accepted response type 1")
		}
		if err != nil {
			t.Fatalf("parse 40-byte type 4 ka2 response: %v", err)
		}
		if parsedType4.tailRefill == nil || *parsedType4.tailRefill != tail {
			t.Fatalf("tail refill = %v, want %x", parsedType4.tailRefill, tail)
		}
	})

	t.Run("logout minimum structure, mock ack and 25 byte variant", func(t *testing.T) {
		if err := parseLogoutACK([]byte{0x04}); err != nil {
			t.Fatalf("parse 1-byte logout response: %v", err)
		}
		if err := parseLogoutACK([]byte{0x04, 0x00, 0x00, 0x00}); err != nil {
			t.Fatalf("parse 4-byte mock logout ack: %v", err)
		}
		if err := parseLogoutACK(peerLogoutResponseVariant(25)); err != nil {
			t.Fatalf("parse 25-byte logout response: %v", err)
		}
	})
}
