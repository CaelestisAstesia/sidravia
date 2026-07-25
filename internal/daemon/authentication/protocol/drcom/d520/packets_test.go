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
	got1, err := parseKA2Response(type1Resp, 0x00, ka2Type1)
	if err != nil {
		t.Fatalf("parse ka2 type1 response: %v", err)
	}
	want1 := mustHex4(t, doc.CryptoIntermediates.KA2TailType1.OutputHex)
	if got1.tail != want1 {
		t.Fatalf("ka2 type1 tail = %x, want %x", got1.tail, want1)
	}

	type3Resp := mustHex(t, exchange(t, doc, "ka2_type3").DeclaredResponseHex)
	got3, err := parseKA2Response(type3Resp, 0x01, ka2Type3)
	if err != nil {
		t.Fatalf("parse ka2 type3 response: %v", err)
	}
	want3 := mustHex4(t, doc.CryptoIntermediates.KA2TailType3.OutputHex)
	if got3.tail != want3 {
		t.Fatalf("ka2 type3 tail = %x, want %x", got3.tail, want3)
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
