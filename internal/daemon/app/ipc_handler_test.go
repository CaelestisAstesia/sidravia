package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/ipc/contract"
)

func TestIPCHandlerRoutesRetainedMethods(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "version", "build")
	for _, method := range []string{contract.MethodSessionEnsureRunning, contract.MethodSessionRestart, contract.MethodSessionRemove} {
		_, publicErr := handler(context.Background(), method, []byte(`{}`))
		if publicErr == nil || publicErr.Code != contract.ErrorCodeInvalidArgument {
			t.Fatalf("%s was not routed to SessionHandler: %#v", method, publicErr)
		}
	}
}

func TestIPCHandlerRoutesAllConfigurationMethods(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "version", "build")
	for _, method := range []string{
		contract.MethodConfigurationGet,
		contract.MethodConfigurationCreate,
		contract.MethodConfigurationUpdate,
		contract.MethodConfigurationSetPassword,
		contract.MethodConfigurationRemove,
		contract.MethodSessionStartConfiguration,
	} {
		_, publicErr := handler(context.Background(), method, []byte(`{}`))
		if publicErr == nil || publicErr.Code != contract.ErrorCodeInvalidArgument {
			t.Fatalf("%s was not routed to its typed handler: %#v", method, publicErr)
		}
	}
	result, publicErr := handler(context.Background(), contract.MethodConfigurationList, []byte(`{}`))
	if publicErr != nil {
		t.Fatal(publicErr)
	}
	var list contract.ConfigurationListResult
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Configurations) != 1 || list.Configurations[0].ConfigurationID != "configuration-1" {
		t.Fatalf("configuration.list = %#v", list)
	}
}

func TestIPCHandlerConfigurationCreateAndStartDoNotExposePassword(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "version", "build")
	const passwordMarker = "private-password-marker"
	create, publicErr := handler(context.Background(), contract.MethodConfigurationCreate, []byte(
		`{"configurationId":"campus","displayName":"","institutionProfileId":"profile-1","username":"user","password":"`+
			passwordMarker+`","allowInsecureStorage":false}`,
	))
	if publicErr != nil {
		t.Fatal(publicErr)
	}
	if strings.Contains(string(create), passwordMarker) || strings.Contains(string(create), `"password"`) {
		t.Fatalf("create result exposed password: %s", create)
	}
	started, publicErr := handler(context.Background(), contract.MethodSessionStartConfiguration, []byte(`{"configurationId":"campus"}`))
	if publicErr != nil {
		t.Fatal(publicErr)
	}
	var sessionResult contract.SessionResult
	if err := json.Unmarshal(started, &sessionResult); err != nil {
		t.Fatal(err)
	}
	if sessionResult.AuthenticationSessionID == "" || sessionResult.AccountName != "user" {
		t.Fatalf("startConfiguration result incomplete: %#v", sessionResult)
	}
	if strings.Contains(string(started), passwordMarker) {
		t.Fatalf("startConfiguration result exposed password: %s", started)
	}
}

func TestIPCHandlerRoutesStatusToStatusHandler(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "0.1.0-dev", "dev")

	result, cerr := handler(context.Background(), contract.MethodDaemonStatus, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("daemon.status error: %+v", cerr)
	}
	var status contract.StatusResult
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("unmarshal status result: %v", err)
	}
	if status.ProductVersion != "0.1.0-dev" {
		t.Errorf("productVersion: got %q, want %q", status.ProductVersion, "0.1.0-dev")
	}
	if status.BuildID != "dev" {
		t.Errorf("buildID: got %q, want %q", status.BuildID, "dev")
	}
	if status.PID <= 0 {
		t.Errorf("pid must be positive, got %d", status.PID)
	}
	if status.Status != "running" {
		t.Errorf("status: got %q, want %q", status.Status, "running")
	}
}

func TestIPCHandlerUnknownMethodReturnsUnknownMethod(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "0.1.0-dev", "dev")

	_, cerr := handler(context.Background(), "daemon.unknown", []byte(`{}`))
	if cerr == nil {
		t.Fatal("expected error for unknown method")
	}
	if cerr.Code != contract.ErrorCodeUnknownMethod {
		t.Errorf("code: got %q, want %q", cerr.Code, contract.ErrorCodeUnknownMethod)
	}
	// No session operation was invoked: the Supervisor has no sessions.
	snapshots, err := setup.supervisor.List(context.Background())
	if err != nil {
		t.Fatalf("supervisor.List() error = %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("unknown method must not create a session, got %d", len(snapshots))
	}
}

// TestIPCHandlerVerticalSequence proves one in-memory vertical sequence:
// applying a network snapshot through Application, starting one-shot
// authentication through IPCHandler, getting the same Session, and stopping it.
// It also confirms all three Session methods reach the existing Session handler
// and that the start result carries the selected binding and authenticating
// state produced by the delegated snapshot.
func TestIPCHandlerVerticalSequence(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "0.1.0-dev", "dev")

	// Apply a usable network snapshot through Application so the one-shot start
	// selects a binding and enters the protocol-start path.
	snapshot := appTestNetworkSnapshot(t, 1)
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot() error = %v", err)
	}

	// Start one-shot authentication through the composed IPC handler.
	startResult, cerr := handler(ctx, contract.MethodSessionStartOneShot, validStartPayload())
	if cerr != nil {
		t.Fatalf("session.startOneShot error: %+v", cerr)
	}
	var started contract.SessionResult
	if err := json.Unmarshal(startResult, &started); err != nil {
		t.Fatalf("unmarshal start result: %v", err)
	}
	if started.AuthenticationSessionID == "" {
		t.Fatal("start returned empty session ID")
	}
	if started.SelectedNetworkBinding == nil {
		t.Fatal("expected selected network binding after applying usable snapshot")
	}
	if started.SelectedNetworkBinding.InterfaceID != "iface-1" {
		t.Errorf("InterfaceID: got %q, want iface-1", started.SelectedNetworkBinding.InterfaceID)
	}
	if started.State != string(session.Authenticating) {
		t.Errorf("State: got %q, want %q", started.State, session.Authenticating)
	}

	// Get the same session through the composed IPC handler.
	getResult, cerr := handler(ctx, contract.MethodSessionGet, []byte(`{"sessionId":"`+started.AuthenticationSessionID+`"}`))
	if cerr != nil {
		t.Fatalf("session.get error: %+v", cerr)
	}
	var got contract.SessionResult
	if err := json.Unmarshal(getResult, &got); err != nil {
		t.Fatalf("unmarshal get result: %v", err)
	}
	if got.AuthenticationSessionID != started.AuthenticationSessionID {
		t.Errorf("get session ID: got %q, want %q", got.AuthenticationSessionID, started.AuthenticationSessionID)
	}

	listResult, cerr := handler(ctx, contract.MethodSessionList, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("session.list error: %+v", cerr)
	}
	var listed contract.SessionListResult
	if err := json.Unmarshal(listResult, &listed); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listed.Sessions) != 1 ||
		listed.Sessions[0].AuthenticationSessionID != started.AuthenticationSessionID {
		t.Fatalf("session.list result = %#v, want started Session", listed.Sessions)
	}

	// Stop the session through the composed IPC handler.
	stopResult, cerr := handler(ctx, contract.MethodSessionStop, []byte(`{"sessionId":"`+started.AuthenticationSessionID+`"}`))
	if cerr != nil {
		t.Fatalf("session.stop error: %+v", cerr)
	}
	var stopped contract.SessionResult
	if err := json.Unmarshal(stopResult, &stopped); err != nil {
		t.Fatalf("unmarshal stop result: %v", err)
	}
	if stopped.AuthenticationSessionID != started.AuthenticationSessionID {
		t.Errorf("stop session ID: got %q, want %q", stopped.AuthenticationSessionID, started.AuthenticationSessionID)
	}
	if stopped.State != string(session.Stopping) {
		t.Errorf("stop State: got %q, want %q", stopped.State, session.Stopping)
	}
}

func TestIPCHandlerListsProfiles(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "0.1.0-dev", "dev")

	result, cerr := handler(context.Background(), contract.MethodProfileList, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("profile.list error: %+v", cerr)
	}
	var listed contract.ProfileListResult
	if err := json.Unmarshal(result, &listed); err != nil {
		t.Fatalf("unmarshal profile list: %v", err)
	}
	if len(listed.Profiles) != 1 {
		t.Fatalf("profile count = %d, want 1", len(listed.Profiles))
	}
	profile := listed.Profiles[0]
	if profile.InstitutionProfileID != "profile-1" ||
		profile.DisplayName != "profile-1 display" ||
		profile.AuthenticationProtocolID != "drcom" {
		t.Fatalf("profile.list result = %#v", profile)
	}
}

// TestIPCHandlerDoesNotLeakPassword proves that successful and failed responses
// from the composed IPC handler never contain the supplied password.
func TestIPCHandlerDoesNotLeakPassword(t *testing.T) {
	ctx := context.Background()
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "0.1.0-dev", "dev")

	const password = "ipc-handler-password-secret"

	// Apply a usable snapshot so the start succeeds and returns a full result.
	snapshot := appTestNetworkSnapshot(t, 1)
	if err := setup.application.ApplySystemNetworkSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("ApplySystemNetworkSnapshot() error = %v", err)
	}

	// A successful start must not expose the password in the result JSON.
	startResult, cerr := handler(ctx, contract.MethodSessionStartOneShot, ipcStartPayload(t, password, "profile-1"))
	if cerr != nil {
		t.Fatalf("session.startOneShot error: %+v", cerr)
	}
	if strings.Contains(string(startResult), password) {
		t.Fatalf("successful result JSON leaks password: %s", startResult)
	}

	// Stop the started session before exercising the failing path.
	var started contract.SessionResult
	if err := json.Unmarshal(startResult, &started); err != nil {
		t.Fatalf("unmarshal start result: %v", err)
	}
	if _, cerr := handler(ctx, contract.MethodSessionStop, []byte(`{"sessionId":"`+started.AuthenticationSessionID+`"}`)); cerr != nil {
		t.Fatalf("session.stop error: %+v", cerr)
	}

	// A failing start (missing profile) must not expose the password in the
	// error message.
	_, cerr = handler(ctx, contract.MethodSessionStartOneShot, ipcStartPayload(t, password, "missing-profile"))
	if cerr == nil {
		t.Fatal("expected error for missing profile")
	}
	if strings.Contains(cerr.Message, password) {
		t.Fatalf("error message leaks password: %q", cerr.Message)
	}
	if cerr.Code != contract.ErrorCodeProfileNotFound {
		t.Errorf("error code: got %q, want %q", cerr.Code, contract.ErrorCodeProfileNotFound)
	}
}

func ipcStartPayload(t *testing.T, password, profileID string) []byte {
	t.Helper()
	data, err := json.Marshal(contract.SessionStartOneShotPayload{
		DisplayName:              "secrecy display",
		InstitutionProfileID:     profileID,
		Username:                 "secrecy-user",
		Password:                 password,
		NetworkBindingPolicyMode: "automatically_select_latest_available",
		ProtocolContextOverride:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}
