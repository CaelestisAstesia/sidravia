package contract_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"sidravia/internal/daemon/app"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

const fixturePath = "testdata/v1/conformance.json"

type conformanceFixture struct {
	SchemaVersion int           `json:"schemaVersion"`
	Enums         fixtureEnums  `json:"enums"`
	Cases         []fixtureCase `json:"cases"`
}

type fixtureEnums struct {
	DaemonModes               []string `json:"daemonModes"`
	SessionStartOutcomes      []string `json:"sessionStartOutcomes"`
	SessionIntents            []string `json:"sessionIntents"`
	SessionStates             []string `json:"sessionStates"`
	StorageProtections        []string `json:"storageProtections"`
	NetworkBindingPolicyModes []string `json:"networkBindingPolicyModes"`
}

type fixtureCase struct {
	Name            string `json:"name"`
	Method          string `json:"method"`
	Request         string `json:"request"`
	SuccessResponse string `json:"successResponse"`
	ErrorResponse   string `json:"errorResponse"`
}

func TestV1ConformanceFixture(t *testing.T) {
	fixture := loadFixture(t)
	if fixture.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", fixture.SchemaVersion)
	}
	assertEnums(t, fixture.Enums)

	expectedMethods := map[string]struct{}{
		contract.MethodDaemonStatus:              {},
		contract.MethodDaemonStop:                {},
		contract.MethodSessionStartOneShot:       {},
		contract.MethodSessionStop:               {},
		contract.MethodSessionEnsureRunning:      {},
		contract.MethodSessionRestart:            {},
		contract.MethodSessionRemove:             {},
		contract.MethodSessionGet:                {},
		contract.MethodSessionList:               {},
		contract.MethodProfileList:               {},
		contract.MethodConfigurationList:         {},
		contract.MethodConfigurationGet:          {},
		contract.MethodConfigurationCreate:       {},
		contract.MethodConfigurationUpdate:       {},
		contract.MethodConfigurationSetPassword:  {},
		contract.MethodConfigurationRemove:       {},
		contract.MethodSessionStartConfiguration: {},
	}
	if len(fixture.Cases) != len(expectedMethods) {
		t.Fatalf("fixture case count = %d, want %d", len(fixture.Cases), len(expectedMethods))
	}

	seenNames := make(map[string]struct{}, len(fixture.Cases))
	seenMethods := make(map[string]struct{}, len(fixture.Cases))
	for _, item := range fixture.Cases {
		if item.Name == "" || item.Method == "" || item.Request == "" || item.SuccessResponse == "" || item.ErrorResponse == "" {
			t.Fatalf("fixture case %q has an empty required field", item.Name)
		}
		if _, duplicate := seenNames[item.Name]; duplicate {
			t.Fatalf("duplicate fixture case name %q", item.Name)
		}
		seenNames[item.Name] = struct{}{}
		if _, known := expectedMethods[item.Method]; !known {
			t.Fatalf("fixture case %q has unknown method %q", item.Name, item.Method)
		}
		if _, duplicate := seenMethods[item.Method]; duplicate {
			t.Fatalf("fixture method %q appears more than once", item.Method)
		}
		seenMethods[item.Method] = struct{}{}

		t.Run(item.Name, func(t *testing.T) {
			request, err := contract.DecodeRequest([]byte(item.Request))
			if err != nil {
				t.Fatalf("request envelope does not decode: %v", err)
			}
			if request.Method != item.Method {
				t.Fatalf("request method = %q, want %q", request.Method, item.Method)
			}
			decodeMethodPayload(t, request.Method, request.Payload)
			assertSuccessResponse(t, item, request.ID)
			assertErrorResponse(t, item, request.ID)
		})
	}
	if !reflect.DeepEqual(seenMethods, expectedMethods) {
		t.Fatal("fixture method set differs from the current v1 method constants")
	}
	assertSecretNegativeSpace(t, fixture)
}

func loadFixture(t *testing.T) conformanceFixture {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture conformanceFixture
	decodeStrictJSON(t, data, &fixture)
	return fixture
}

func decodeStrictJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		t.Fatal("JSON value is missing")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("strict JSON decode: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatal("JSON has trailing data")
	}
}

func assertEnums(t *testing.T, enums fixtureEnums) {
	t.Helper()
	assertEnum(t, "daemonModes", enums.DaemonModes, []string{string(launchcontract.ModeHeadless), string(launchcontract.ModeDesktop)})
	assertEnum(t, "sessionStartOutcomes", enums.SessionStartOutcomes, []string{app.SessionStartCreated, app.SessionStartAlreadyRunning, app.SessionStartResumed})
	assertEnum(t, "sessionIntents", enums.SessionIntents, []string{string(session.MaintainAuthentication), string(session.SuspendAuthentication)})
	assertEnum(t, "sessionStates", enums.SessionStates, []string{
		string(session.Suspended), string(session.WaitingForNetwork), string(session.Authenticating), string(session.Authenticated),
		string(session.WaitingBeforeRetry), string(session.BlockedByError), string(session.Stopping),
	})
	assertEnum(t, "storageProtections", enums.StorageProtections, []string{string(jsonfile.ProtectionProtected), string(jsonfile.ProtectionUnprotected)})
	assertEnum(t, "networkBindingPolicyModes", enums.NetworkBindingPolicyModes, []string{string(session.AutomaticallySelectLatestAvailable)})
}

func assertEnum(t *testing.T, name string, got, want []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(got))
	for _, value := range got {
		if _, duplicate := seen[value]; duplicate {
			t.Fatalf("enum %s contains duplicate %q", name, value)
		}
		seen[value] = struct{}{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("enum %s differs from its canonical order", name)
	}
}

func decodeMethodPayload(t *testing.T, method string, payload json.RawMessage) {
	t.Helper()
	var err error
	switch method {
	case contract.MethodDaemonStatus, contract.MethodDaemonStop, contract.MethodSessionList, contract.MethodProfileList, contract.MethodConfigurationList:
		err = contract.DecodeEmptyPayload(payload)
	case contract.MethodSessionStartOneShot:
		_, err = contract.DecodeSessionStartOneShotPayload(payload)
	case contract.MethodSessionStop:
		_, err = contract.DecodeSessionStopPayload(payload)
	case contract.MethodSessionEnsureRunning:
		_, err = contract.DecodeSessionEnsureRunningPayload(payload)
	case contract.MethodSessionRestart:
		_, err = contract.DecodeSessionRestartPayload(payload)
	case contract.MethodSessionRemove:
		_, err = contract.DecodeSessionRemovePayload(payload)
	case contract.MethodSessionGet:
		_, err = contract.DecodeSessionGetPayload(payload)
	case contract.MethodSessionStartConfiguration, contract.MethodConfigurationGet, contract.MethodConfigurationRemove:
		_, err = contract.DecodeConfigurationIDPayload(payload)
	case contract.MethodConfigurationCreate:
		_, err = contract.DecodeConfigurationCreatePayload(payload)
	case contract.MethodConfigurationUpdate:
		_, err = contract.DecodeConfigurationUpdatePayload(payload)
	case contract.MethodConfigurationSetPassword:
		_, err = contract.DecodeConfigurationSetPasswordPayload(payload)
	default:
		t.Fatalf("no payload decoder for %q", method)
	}
	if err != nil {
		t.Fatalf("method payload does not decode: %v", err)
	}
}

func assertSuccessResponse(t *testing.T, item fixtureCase, requestID string) {
	t.Helper()
	response, err := contract.DecodeResponse([]byte(item.SuccessResponse))
	if err != nil {
		t.Fatalf("success envelope does not decode: %v", err)
	}
	if !response.OK || response.ID != requestID || response.Error != nil || response.Result == nil {
		t.Fatal("success response does not match its request")
	}
	requireObjectKeys(t, []byte(item.SuccessResponse), "kind", "id", "ok", "result")

	switch item.Method {
	case contract.MethodDaemonStatus:
		var result contract.StatusResult
		decodeStrictJSON(t, response.Result, &result)
		if result.ProductVersion == "" || result.BuildID == "" || result.PID <= 0 || result.Status != "running" || result.Mode != string(launchcontract.ModeHeadless) || result.DesktopOwnerPID != nil {
			t.Fatal("headless status result is incomplete or exposes a desktop owner")
		}
	case contract.MethodDaemonStop:
		var result contract.DaemonStopResult
		decodeStrictJSON(t, response.Result, &result)
		if result.Status != "stopping" {
			t.Fatal("daemon stop result is not stopping")
		}
	case contract.MethodSessionStartOneShot, contract.MethodSessionEnsureRunning, contract.MethodSessionStartConfiguration:
		var result contract.SessionStartResult
		decodeStrictJSON(t, response.Result, &result)
		assertSessionResult(t, result.Session, item.Method != contract.MethodSessionStartOneShot, item.Method == contract.MethodSessionStartConfiguration)
		if result.Outcome == "" {
			t.Fatal("session start result has no outcome")
		}
	case contract.MethodSessionStop, contract.MethodSessionRestart, contract.MethodSessionGet:
		var result contract.SessionResult
		decodeStrictJSON(t, response.Result, &result)
		assertSessionResult(t, result, true, item.Method == contract.MethodSessionGet)
	case contract.MethodSessionRemove:
		var result contract.SessionRemoveResult
		decodeStrictJSON(t, response.Result, &result)
		if result.SessionID == "" || result.Status != "removed" {
			t.Fatal("session remove result is incomplete")
		}
	case contract.MethodSessionList:
		var result contract.SessionListResult
		decodeStrictJSON(t, response.Result, &result)
		if result.Sessions == nil || len(result.Sessions) != 0 {
			t.Fatal("session list must be an empty, non-null array")
		}
	case contract.MethodProfileList:
		var result contract.ProfileListResult
		decodeStrictJSON(t, response.Result, &result)
		if result.Profiles == nil || len(result.Profiles) != 0 {
			t.Fatal("profile list must be an empty, non-null array")
		}
	case contract.MethodConfigurationList:
		var result contract.ConfigurationListResult
		decodeStrictJSON(t, response.Result, &result)
		if result.StorageProtection != string(jsonfile.ProtectionProtected) || result.Configurations == nil || len(result.Configurations) != 0 {
			t.Fatal("configuration list must retain protection and an empty, non-null array")
		}
	case contract.MethodConfigurationGet, contract.MethodConfigurationCreate, contract.MethodConfigurationUpdate, contract.MethodConfigurationSetPassword:
		var result contract.ConfigurationResult
		decodeStrictJSON(t, response.Result, &result)
		assertConfigurationResult(t, result, item.Method == contract.MethodConfigurationCreate)
	case contract.MethodConfigurationRemove:
		var result contract.ConfigurationRemoveResult
		decodeStrictJSON(t, response.Result, &result)
		if result.ConfigurationID == "" || result.Status != "removed" {
			t.Fatal("configuration remove result is incomplete")
		}
	default:
		t.Fatalf("no success result decoder for %q", item.Method)
	}
}

func assertSessionResult(t *testing.T, result contract.SessionResult, requireConfiguration, requireAllOptional bool) {
	t.Helper()
	if result.AuthenticationSessionID == "" || result.DisplayName == "" || result.InstitutionProfileID == "" || result.InstitutionDisplayName == "" || result.AuthenticationProtocolID == "" || result.AccountName == "" || result.Intent == "" || result.State == "" || result.UpdatedAt == "" {
		t.Fatal("session result omits a required public field")
	}
	if requireConfiguration && result.ConfigurationID == "" {
		t.Fatal("retained session omits configurationId")
	}
	if !requireConfiguration && result.ConfigurationID != "" {
		t.Fatal("one-shot session includes configurationId")
	}
	if requireAllOptional && (result.StateReason == nil || result.SelectedNetworkBinding == nil || result.AuthenticationEstablishedAt == nil || result.NextRetryAt == nil || result.LastAuthenticationFailure == nil) {
		t.Fatal("full retained session omits an optional public field")
	}
}

func assertConfigurationResult(t *testing.T, result contract.ConfigurationResult, generatedID bool) {
	t.Helper()
	if result.ConfigurationID == "" || result.DisplayName == "" || result.InstitutionProfileID == "" || result.InstitutionDisplayName == "" || result.AuthenticationProtocolID == "" || result.Username == "" || result.StorageProtection == "" {
		t.Fatal("configuration result omits a required public field")
	}
	if generatedID && result.ConfigurationID != "cfg-0123456789abcdef0123456789abcdef" {
		t.Fatal("configuration create result does not freeze the generated ID shape")
	}
}

func assertErrorResponse(t *testing.T, item fixtureCase, requestID string) {
	t.Helper()
	response, err := contract.DecodeResponse([]byte(item.ErrorResponse))
	if err != nil {
		t.Fatalf("error envelope does not decode: %v", err)
	}
	if response.OK || response.ID != requestID || response.Error == nil || response.Result != nil {
		t.Fatal("error response does not contain only its public error")
	}
	requireObjectKeys(t, []byte(item.ErrorResponse), "kind", "id", "ok", "error")
	want := reachableError(item.Method)
	if response.Error.Code != want.Code || response.Error.Message != want.Message {
		t.Fatalf("error fixture for %q is not a reachable public error", item.Method)
	}
}

func reachableError(method string) contract.Error {
	switch method {
	case contract.MethodDaemonStatus, contract.MethodDaemonStop:
		return contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "malformed daemon payload"}
	case contract.MethodSessionStartOneShot:
		return contract.Error{Code: contract.ErrorCodeProfileNotFound, Message: "institution profile not found"}
	case contract.MethodSessionStop, contract.MethodSessionEnsureRunning, contract.MethodSessionRemove, contract.MethodSessionGet:
		return contract.Error{Code: contract.ErrorCodeSessionNotFound, Message: "session not found"}
	case contract.MethodSessionRestart:
		return contract.Error{Code: contract.ErrorCodeSessionStateConflict, Message: "session state does not allow operation"}
	case contract.MethodSessionList:
		return contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "session operation failed"}
	case contract.MethodProfileList:
		return contract.Error{Code: contract.ErrorCodeProfileOperationFailed, Message: "profile operation failed"}
	case contract.MethodConfigurationList:
		return contract.Error{Code: contract.ErrorCodeConfigurationOperationFailed, Message: "configuration operation failed"}
	case contract.MethodConfigurationGet, contract.MethodConfigurationRemove, contract.MethodSessionStartConfiguration:
		return contract.Error{Code: contract.ErrorCodeConfigurationNotFound, Message: "configuration not found"}
	case contract.MethodConfigurationCreate:
		return contract.Error{Code: contract.ErrorCodeConfigurationConflict, Message: "configuration already exists"}
	case contract.MethodConfigurationUpdate:
		return contract.Error{Code: contract.ErrorCodeConfigurationAutoLoginConflict, Message: "another configuration already enables automatic login"}
	case contract.MethodConfigurationSetPassword:
		return contract.Error{Code: contract.ErrorCodeInsecureStorageConfirmationRequired, Message: "insecure storage confirmation required"}
	default:
		return contract.Error{}
	}
}

func requireObjectKeys(t *testing.T, data []byte, keys ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	decodeStrictJSON(t, data, &object)
	if len(object) != len(keys) {
		t.Fatal("wire object has an unexpected field count")
	}
	for _, key := range keys {
		if _, present := object[key]; !present {
			t.Fatalf("wire object omits %q", key)
		}
	}
}

func assertSecretNegativeSpace(t *testing.T, fixture conformanceFixture) {
	t.Helper()
	const oneShotMarker = "fixture-one-shot-password"
	const configurationMarker = "fixture-configuration-password"
	requests := make([]string, 0, len(fixture.Cases))
	for _, item := range fixture.Cases {
		requests = append(requests, item.Request)
		if strings.Contains(item.SuccessResponse, oneShotMarker) || strings.Contains(item.SuccessResponse, configurationMarker) || strings.Contains(item.ErrorResponse, oneShotMarker) || strings.Contains(item.ErrorResponse, configurationMarker) {
			t.Fatal("fixture response contains a request secret marker")
		}
		for _, forbidden := range []string{"diagnosticCause", "credentialId", "protocolContextOverride", "password"} {
			if strings.Contains(item.SuccessResponse, `"`+forbidden+`"`) || strings.Contains(item.ErrorResponse, `"`+forbidden+`"`) {
				t.Fatal("fixture response contains a forbidden secret or diagnostic field")
			}
		}
		if strings.Contains(item.Name, oneShotMarker) || strings.Contains(item.Name, configurationMarker) || strings.Contains(item.Method, oneShotMarker) || strings.Contains(item.Method, configurationMarker) {
			t.Fatal("fixture secret marker escaped the request strings")
		}
	}
	if strings.Count(strings.Join(requests, ""), oneShotMarker) != 1 || strings.Count(strings.Join(requests, ""), configurationMarker) != 1 {
		t.Fatal("fixture request secret markers must each occur exactly once")
	}
	enums, err := json.Marshal(fixture.Enums)
	if err != nil {
		t.Fatalf("marshal enums: %v", err)
	}
	if strings.Contains(string(enums), oneShotMarker) || strings.Contains(string(enums), configurationMarker) {
		t.Fatal("fixture enum contains a request secret marker")
	}
}
