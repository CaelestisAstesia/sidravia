package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/ipc/contract"
)

type fakeSessionApplication struct {
	snapshot  session.Snapshot
	snapshots []session.Snapshot
	err       error

	lastStartInput OneShotAuthenticationInput
	startCalls     int
	lastConfigID   config.ConfigurationID
	configCalls    int
	lastStopID     session.AuthenticationSessionID
	stopCalls      int
	lastGetID      session.AuthenticationSessionID
	getCalls       int
	listCalls      int
	lastEnsureID   session.AuthenticationSessionID
	ensureCalls    int
	lastRestartID  session.AuthenticationSessionID
	restartCalls   int
	lastRemoveID   session.AuthenticationSessionID
	removeCalls    int
}

func (fake *fakeSessionApplication) ListSessions(context.Context) ([]session.Snapshot, error) {
	fake.listCalls++
	if fake.err != nil {
		return nil, fake.err
	}
	return append([]session.Snapshot(nil), fake.snapshots...), nil
}

func (fake *fakeSessionApplication) StartOneShotAuthentication(ctx context.Context, input OneShotAuthenticationInput) (SessionStartResult, error) {
	fake.startCalls++
	fake.lastStartInput = input
	if fake.err != nil {
		return SessionStartResult{}, fake.err
	}
	return SessionStartResult{SessionID: fake.snapshot.AuthenticationSessionID, Snapshot: fake.snapshot, Outcome: SessionStartCreated}, nil
}
func (fake *fakeSessionApplication) StartConfigurationAuthentication(_ context.Context, id config.ConfigurationID) (SessionStartResult, error) {
	fake.configCalls++
	fake.lastConfigID = id
	if fake.err != nil {
		return SessionStartResult{}, fake.err
	}
	return SessionStartResult{SessionID: fake.snapshot.AuthenticationSessionID, Snapshot: fake.snapshot, Outcome: SessionStartCreated}, nil
}

func TestSessionHandlerStartConfigurationReturnsCompleteSessionResult(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	result, publicErr := SessionHandler(fake)(
		context.Background(),
		contract.MethodSessionStartConfiguration,
		[]byte(`{"configurationId":"campus"}`),
	)
	if publicErr != nil {
		t.Fatal(publicErr)
	}
	if fake.configCalls != 1 || fake.lastConfigID != "campus" || fake.startCalls != 0 {
		t.Fatalf("route calls config=%d id=%q one-shot=%d", fake.configCalls, fake.lastConfigID, fake.startCalls)
	}
	var got contract.SessionStartResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "created" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	assertSessionResultMatchesSnapshot(t, got.Session, fullSnapshot())
}

func TestSessionHandlerStartConfigurationRejectsMalformedAndHidesFailure(t *testing.T) {
	for _, payload := range []string{"", "null", `{}`, `{"configurationId":""}`, `{"configurationId":"campus","extra":true}`, `{"configurationId":"campus"}{}`, `{"configurationId":"campus"}!`} {
		fake := &fakeSessionApplication{snapshot: fullSnapshot()}
		_, publicErr := SessionHandler(fake)(context.Background(), contract.MethodSessionStartConfiguration, []byte(payload))
		if publicErr == nil || publicErr.Code != contract.ErrorCodeInvalidArgument || fake.configCalls != 0 {
			t.Fatalf("payload %q: error=%#v calls=%d", payload, publicErr, fake.configCalls)
		}
	}
	fake := &fakeSessionApplication{err: errors.New("wrapped-cause-marker")}
	_, publicErr := SessionHandler(fake)(context.Background(), contract.MethodSessionStartConfiguration, []byte(`{"configurationId":"private-id"}`))
	if publicErr == nil || publicErr.Code != contract.ErrorCodeSessionOperationFailed ||
		publicErr.Message != "session operation failed" ||
		strings.Contains(publicErr.Message, "private-id") ||
		strings.Contains(publicErr.Message, "wrapped-cause-marker") {
		t.Fatalf("unsafe startConfiguration error: %#v", publicErr)
	}
}

func (fake *fakeSessionApplication) StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	fake.stopCalls++
	fake.lastStopID = sessionID
	if fake.err != nil {
		return session.Snapshot{}, fake.err
	}
	return fake.snapshot, nil
}

func (fake *fakeSessionApplication) EnsureSessionRunning(_ context.Context, id session.AuthenticationSessionID) (session.Snapshot, error) {
	fake.ensureCalls++
	fake.lastEnsureID = id
	return fake.snapshot, fake.err
}

func (fake *fakeSessionApplication) RestartSession(_ context.Context, id session.AuthenticationSessionID) (session.Snapshot, error) {
	fake.restartCalls++
	fake.lastRestartID = id
	return fake.snapshot, fake.err
}

func (fake *fakeSessionApplication) RemoveSession(_ context.Context, id session.AuthenticationSessionID) error {
	fake.removeCalls++
	fake.lastRemoveID = id
	return fake.err
}

func TestSessionHandlerRoutesRetainedLifecycleMethods(t *testing.T) {
	for _, test := range []struct {
		method string
		check  func(*fakeSessionApplication) (int, session.AuthenticationSessionID)
		remove bool
	}{
		{contract.MethodSessionEnsureRunning, func(f *fakeSessionApplication) (int, session.AuthenticationSessionID) {
			return f.ensureCalls, f.lastEnsureID
		}, false},
		{contract.MethodSessionRestart, func(f *fakeSessionApplication) (int, session.AuthenticationSessionID) {
			return f.restartCalls, f.lastRestartID
		}, false},
		{contract.MethodSessionRemove, func(f *fakeSessionApplication) (int, session.AuthenticationSessionID) {
			return f.removeCalls, f.lastRemoveID
		}, true},
	} {
		fake := &fakeSessionApplication{snapshot: fullSnapshot()}
		result, publicErr := SessionHandler(fake)(context.Background(), test.method, []byte(`{"sessionId":"session-1"}`))
		if publicErr != nil {
			t.Fatalf("%s: %v", test.method, publicErr)
		}
		calls, id := test.check(fake)
		if calls != 1 || id != "session-1" {
			t.Fatalf("%s routed calls=%d id=%q", test.method, calls, id)
		}
		if test.remove {
			if string(result) != `{"sessionId":"session-1","status":"removed"}` {
				t.Fatalf("%s result=%s", test.method, result)
			}
			continue
		}
		var got contract.SessionResult
		if err := json.Unmarshal(result, &got); err != nil {
			t.Fatalf("%s decode result: %v", test.method, err)
		}
		assertSessionResultMatchesSnapshot(t, got, fullSnapshot())
	}
}

func assertSessionResultMatchesSnapshot(t *testing.T, got contract.SessionResult, snap session.Snapshot) {
	t.Helper()
	expectedResult, publicErr := SessionHandler(&fakeSessionApplication{snapshot: snap})(
		context.Background(), contract.MethodSessionGet, []byte(`{"sessionId":"session-1"}`),
	)
	if publicErr != nil {
		t.Fatalf("build expected SessionResult: %v", publicErr)
	}
	var want contract.SessionResult
	if err := json.Unmarshal(expectedResult, &want); err != nil {
		t.Fatalf("decode expected SessionResult: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SessionResult mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSessionHandlerRetainedFailureIsStatic(t *testing.T) {
	const suppliedID = "secret-session-id"
	const cause = "injected-cause-marker"
	for _, method := range []string{contract.MethodSessionEnsureRunning, contract.MethodSessionRestart, contract.MethodSessionRemove} {
		fake := &fakeSessionApplication{err: errors.New(cause)}
		_, publicErr := SessionHandler(fake)(context.Background(), method, []byte(`{"sessionId":"`+suppliedID+`"}`))
		if publicErr == nil || publicErr.Code != contract.ErrorCodeSessionOperationFailed || publicErr.Message != "session operation failed" {
			t.Fatalf("%s error=%#v", method, publicErr)
		}
		if strings.Contains(publicErr.Message, suppliedID) || strings.Contains(publicErr.Message, cause) {
			t.Fatalf("%s leaked failure material: %q", method, publicErr.Message)
		}
	}
}

func (fake *fakeSessionApplication) GetSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error) {
	fake.getCalls++
	fake.lastGetID = sessionID
	if fake.err != nil {
		return session.Snapshot{}, fake.err
	}
	return fake.snapshot, nil
}

func fullSnapshot() session.Snapshot {
	established := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	retry := time.Date(2026, 7, 24, 10, 5, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 24, 10, 0, 1, 0, time.UTC)
	addr := netip.MustParseAddr("10.0.0.2")
	return session.Snapshot{
		AuthenticationSessionID:  "sess-1",
		DisplayName:              "Library WiFi",
		InstitutionProfileID:     "profile-1",
		InstitutionDisplayName:   "Library",
		AuthenticationProtocolID: protocol.AuthenticationProtocolID("drcom"),
		AccountName:              "public-account-label",
		Intent:                   session.MaintainAuthentication,
		State:                    session.Authenticated,
		StateReason:              &session.StateReason{Code: session.StateReasonCodeNetworkUnavailable, Description: "no network"},
		SelectedNetworkBinding: &session.NetworkBindingSummary{
			InterfaceID:      "iface-1",
			DisplayName:      "Eth0",
			LocalIPv4Address: addr,
		},
		AuthenticationEstablishedAt: &established,
		NextRetryAt:                 &retry,
		LastAuthenticationFailure: &session.AuthenticationFailure{
			Code:                   protocol.AuthenticationProtocolFailureCode("credentials_rejected"),
			Description:            "bad credentials",
			HandlingRecommendation: protocol.RetryAfterStandardDelay,
		},
		Revision:  7,
		UpdatedAt: updated,
	}
}

func validStartPayload() []byte {
	return []byte(`{"displayName":"Library WiFi","institutionProfileId":"profile-1","username":"SECRET-USER","password":"SECRET-PASS","networkBindingPolicyMode":"automatically_select_latest_available","protocolContextOverride":{"marker":"OVERRIDE-MARKER"}}`)
}

func TestSessionHandlerStartCallsFakeOnceWithConvertedValues(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	handler := SessionHandler(fake)

	result, cerr := handler(context.Background(), contract.MethodSessionStartOneShot, validStartPayload())
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	if fake.startCalls != 1 {
		t.Fatalf("expected start called once, got %d", fake.startCalls)
	}
	if fake.stopCalls != 0 || fake.getCalls != 0 {
		t.Fatalf("expected stop/get not called, got stop=%d get=%d", fake.stopCalls, fake.getCalls)
	}

	input := fake.lastStartInput
	if input.DisplayName != "Library WiFi" {
		t.Errorf("displayName: got %q", input.DisplayName)
	}
	if input.InstitutionProfileID != config.InstitutionProfileID("profile-1") {
		t.Errorf("institutionProfileId: got %q", input.InstitutionProfileID)
	}
	if input.AuthenticationCredential.Username != "SECRET-USER" {
		t.Errorf("username: got %q", input.AuthenticationCredential.Username)
	}
	if input.AuthenticationCredential.Password != "SECRET-PASS" {
		t.Errorf("password: got %q", input.AuthenticationCredential.Password)
	}
	if input.NetworkBindingPolicy.Mode != session.AutomaticallySelectLatestAvailable {
		t.Errorf("binding mode: got %q", input.NetworkBindingPolicy.Mode)
	}
	if string(input.ProtocolContextOverride) != `{"marker":"OVERRIDE-MARKER"}` {
		t.Errorf("protocolContextOverride: got %q", string(input.ProtocolContextOverride))
	}

	var sr contract.SessionStartResult
	if err := json.Unmarshal(result, &sr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if sr.Session.AuthenticationSessionID != "sess-1" || sr.Outcome != "created" {
		t.Errorf("start result: %#v", sr)
	}
}

func TestSessionHandlerStopCallsFakeOnceWithSessionID(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	handler := SessionHandler(fake)

	result, cerr := handler(context.Background(), contract.MethodSessionStop, []byte(`{"sessionId":"sess-1"}`))
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("expected stop called once, got %d", fake.stopCalls)
	}
	if fake.startCalls != 0 || fake.getCalls != 0 {
		t.Fatalf("expected start/get not called, got start=%d get=%d", fake.startCalls, fake.getCalls)
	}
	if fake.lastStopID != session.AuthenticationSessionID("sess-1") {
		t.Errorf("stop session id: got %q", fake.lastStopID)
	}
	var sr contract.SessionResult
	if err := json.Unmarshal(result, &sr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if sr.AuthenticationSessionID != "sess-1" {
		t.Errorf("sessionId: got %q, want sess-1", sr.AuthenticationSessionID)
	}
}

func TestSessionHandlerGetCallsFakeOnceWithSessionID(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	handler := SessionHandler(fake)

	result, cerr := handler(context.Background(), contract.MethodSessionGet, []byte(`{"sessionId":"sess-1"}`))
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	if fake.getCalls != 1 {
		t.Fatalf("expected get called once, got %d", fake.getCalls)
	}
	if fake.startCalls != 0 || fake.stopCalls != 0 {
		t.Fatalf("expected start/stop not called, got start=%d stop=%d", fake.startCalls, fake.stopCalls)
	}
	if fake.lastGetID != session.AuthenticationSessionID("sess-1") {
		t.Errorf("get session id: got %q", fake.lastGetID)
	}
	var sr contract.SessionResult
	if err := json.Unmarshal(result, &sr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if sr.AuthenticationSessionID != "sess-1" {
		t.Errorf("sessionId: got %q, want sess-1", sr.AuthenticationSessionID)
	}
}

func TestSessionHandlerListCallsFakeOnceAndMapsAllEntries(t *testing.T) {
	second := fullSnapshot()
	second.AuthenticationSessionID = "sess-2"
	second.State = session.Suspended
	fake := &fakeSessionApplication{snapshots: []session.Snapshot{fullSnapshot(), second}}
	handler := SessionHandler(fake)

	result, cerr := handler(context.Background(), contract.MethodSessionList, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	if fake.listCalls != 1 {
		t.Fatalf("ListSessions calls = %d, want 1", fake.listCalls)
	}
	if fake.startCalls != 0 || fake.stopCalls != 0 || fake.getCalls != 0 {
		t.Fatal("Session list invoked another operation")
	}
	var decoded contract.SessionListResult
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal Session list: %v", err)
	}
	if len(decoded.Sessions) != 2 ||
		decoded.Sessions[0].AuthenticationSessionID != "sess-1" ||
		decoded.Sessions[1].AuthenticationSessionID != "sess-2" {
		t.Fatalf("Session list = %#v", decoded.Sessions)
	}
}

func TestSessionHandlerListPreservesEmptyArray(t *testing.T) {
	fake := &fakeSessionApplication{}
	result, cerr := SessionHandler(fake)(context.Background(), contract.MethodSessionList, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	if string(result) != `{"sessions":[]}` {
		t.Errorf("empty Session list = %s", result)
	}
}

func TestSessionHandlerMapsAllSnapshotFields(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	handler := SessionHandler(fake)

	result, cerr := handler(context.Background(), contract.MethodSessionGet, []byte(`{"sessionId":"sess-1"}`))
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	var sr contract.SessionResult
	if err := json.Unmarshal(result, &sr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	snap := fullSnapshot()
	if sr.AuthenticationSessionID != string(snap.AuthenticationSessionID) {
		t.Errorf("sessionId: got %q, want %q", sr.AuthenticationSessionID, snap.AuthenticationSessionID)
	}
	if sr.DisplayName != snap.DisplayName {
		t.Errorf("displayName: got %q, want %q", sr.DisplayName, snap.DisplayName)
	}
	if sr.InstitutionProfileID != string(snap.InstitutionProfileID) {
		t.Errorf("institutionProfileId: got %q, want %q", sr.InstitutionProfileID, snap.InstitutionProfileID)
	}
	if sr.InstitutionDisplayName != snap.InstitutionDisplayName {
		t.Errorf("institutionDisplayName: got %q, want %q", sr.InstitutionDisplayName, snap.InstitutionDisplayName)
	}
	if sr.AuthenticationProtocolID != string(snap.AuthenticationProtocolID) {
		t.Errorf("authenticationProtocolId: got %q, want %q", sr.AuthenticationProtocolID, snap.AuthenticationProtocolID)
	}
	if sr.AccountName != snap.AccountName {
		t.Errorf("accountName: got %q, want %q", sr.AccountName, snap.AccountName)
	}
	if sr.Intent != string(snap.Intent) {
		t.Errorf("intent: got %q, want %q", sr.Intent, snap.Intent)
	}
	if sr.State != string(snap.State) {
		t.Errorf("state: got %q, want %q", sr.State, snap.State)
	}
	if sr.Revision != snap.Revision {
		t.Errorf("revision: got %d, want %d", sr.Revision, snap.Revision)
	}
	if sr.UpdatedAt != snap.UpdatedAt.Format(time.RFC3339Nano) {
		t.Errorf("updatedAt: got %q, want %q", sr.UpdatedAt, snap.UpdatedAt.Format(time.RFC3339Nano))
	}
	if sr.StateReason == nil || sr.StateReason.Code != snap.StateReason.Code || sr.StateReason.Description != snap.StateReason.Description {
		t.Errorf("stateReason: got %+v, want %+v", sr.StateReason, snap.StateReason)
	}
	if sr.SelectedNetworkBinding == nil ||
		sr.SelectedNetworkBinding.InterfaceID != string(snap.SelectedNetworkBinding.InterfaceID) ||
		sr.SelectedNetworkBinding.DisplayName != snap.SelectedNetworkBinding.DisplayName ||
		sr.SelectedNetworkBinding.LocalIPv4Address != snap.SelectedNetworkBinding.LocalIPv4Address.String() {
		t.Errorf("selectedNetworkBinding: got %+v", sr.SelectedNetworkBinding)
	}
	if sr.AuthenticationEstablishedAt == nil || *sr.AuthenticationEstablishedAt != snap.AuthenticationEstablishedAt.Format(time.RFC3339Nano) {
		t.Errorf("authenticationEstablishedAt: got %+v", sr.AuthenticationEstablishedAt)
	}
	if sr.NextRetryAt == nil || *sr.NextRetryAt != snap.NextRetryAt.Format(time.RFC3339Nano) {
		t.Errorf("nextRetryAt: got %+v", sr.NextRetryAt)
	}
	if sr.LastAuthenticationFailure == nil ||
		sr.LastAuthenticationFailure.Code != string(snap.LastAuthenticationFailure.Code) ||
		sr.LastAuthenticationFailure.Description != snap.LastAuthenticationFailure.Description ||
		sr.LastAuthenticationFailure.HandlingRecommendation != string(snap.LastAuthenticationFailure.HandlingRecommendation) {
		t.Errorf("lastAuthenticationFailure: got %+v", sr.LastAuthenticationFailure)
	}
}

func TestSessionHandlerMalformedPayloadDoesNotCallFake(t *testing.T) {
	for _, method := range []string{contract.MethodSessionStartOneShot, contract.MethodSessionStop, contract.MethodSessionGet, contract.MethodSessionList} {
		fake := &fakeSessionApplication{snapshot: fullSnapshot()}
		handler := SessionHandler(fake)
		_, cerr := handler(context.Background(), method, []byte(`{"username":""}`))
		if cerr == nil {
			t.Fatalf("%s: expected error", method)
		}
		if cerr.Code != contract.ErrorCodeInvalidArgument {
			t.Errorf("%s: code: got %q, want %q", method, cerr.Code, contract.ErrorCodeInvalidArgument)
		}
		if fake.startCalls != 0 || fake.stopCalls != 0 || fake.getCalls != 0 || fake.listCalls != 0 {
			t.Fatalf("%s: fake must not be called, got start=%d stop=%d get=%d list=%d", method, fake.startCalls, fake.stopCalls, fake.getCalls, fake.listCalls)
		}
	}
}

func TestSessionHandlerUnknownMethodDoesNotCallFake(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	handler := SessionHandler(fake)
	_, cerr := handler(context.Background(), "session.unknown", []byte(`{}`))
	if cerr == nil {
		t.Fatal("expected error for unknown method")
	}
	if cerr.Code != contract.ErrorCodeUnknownMethod {
		t.Errorf("code: got %q, want %q", cerr.Code, contract.ErrorCodeUnknownMethod)
	}
	if fake.startCalls != 0 || fake.stopCalls != 0 || fake.getCalls != 0 || fake.listCalls != 0 {
		t.Fatalf("fake must not be called for unknown method, got start=%d stop=%d get=%d list=%d", fake.startCalls, fake.stopCalls, fake.getCalls, fake.listCalls)
	}
}

func TestSessionHandlerMapsResolutionFailures(t *testing.T) {
	cases := []struct {
		name     string
		code     ResolutionFailureCode
		wantCode string
		wantMsg  string
	}{
		{"profile not found", ProfileNotFound, contract.ErrorCodeProfileNotFound, "institution profile not found"},
		{"protocol not found", ProtocolNotFound, contract.ErrorCodeProtocolNotFound, "authentication protocol not found"},
		{"configuration not found", ConfigurationNotFound, contract.ErrorCodeConfigurationNotFound, "configuration not found"},
		{"invalid configuration", InvalidConfiguration, contract.ErrorCodeInvalidArgument, "invalid session request"},
		{"invalid environment", InvalidEnvironment, contract.ErrorCodeInvalidArgument, "invalid session request"},
	}
	// The cause carries every secret category the public message must never echo.
	cause := errors.New("diagnostic LEAK-MARKER SECRET-USER SECRET-PASS OVERRIDE-MARKER")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSessionApplication{err: NewResolutionFailure(tc.code, cause)}
			handler := SessionHandler(fake)
			_, cerr := handler(context.Background(), contract.MethodSessionStartOneShot, validStartPayload())
			if cerr == nil {
				t.Fatal("expected error")
			}
			if cerr.Code != tc.wantCode {
				t.Errorf("code: got %q, want %q", cerr.Code, tc.wantCode)
			}
			if cerr.Message != tc.wantMsg {
				t.Errorf("message: got %q, want %q", cerr.Message, tc.wantMsg)
			}
			assertNoSecretsInError(t, cerr.Message)
		})
	}
}

func TestSessionHandlerGenericFailureMapsToSessionOperationFailed(t *testing.T) {
	cause := errors.New("supervisor failure LEAK-MARKER SECRET-USER SECRET-PASS OVERRIDE-MARKER")
	fake := &fakeSessionApplication{err: cause}
	handler := SessionHandler(fake)
	_, cerr := handler(context.Background(), contract.MethodSessionStop, []byte(`{"sessionId":"sess-1"}`))
	if cerr == nil {
		t.Fatal("expected error")
	}
	if cerr.Code != contract.ErrorCodeSessionOperationFailed {
		t.Errorf("code: got %q, want %q", cerr.Code, contract.ErrorCodeSessionOperationFailed)
	}
	if cerr.Message != "session operation failed" {
		t.Errorf("message: got %q, want %q", cerr.Message, "session operation failed")
	}
	assertNoSecretsInError(t, cerr.Message)
}

func assertNoSecretsInError(t *testing.T, message string) {
	t.Helper()
	for _, secret := range []string{"LEAK-MARKER", "SECRET-USER", "SECRET-PASS", "OVERRIDE-MARKER"} {
		if strings.Contains(message, secret) {
			t.Errorf("error message leaked %q: %q", secret, message)
		}
	}
}

func TestSessionHandlerResultDoesNotLeakSecrets(t *testing.T) {
	fake := &fakeSessionApplication{snapshot: fullSnapshot()}
	handler := SessionHandler(fake)
	result, cerr := handler(context.Background(), contract.MethodSessionStartOneShot, validStartPayload())
	if cerr != nil {
		t.Fatalf("unexpected error: %+v", cerr)
	}
	text := string(result)
	for _, secret := range []string{"SECRET-USER", "SECRET-PASS", "OVERRIDE-MARKER", "credentialId"} {
		if strings.Contains(text, secret) {
			t.Errorf("result JSON leaked %q: %s", secret, text)
		}
	}
}
