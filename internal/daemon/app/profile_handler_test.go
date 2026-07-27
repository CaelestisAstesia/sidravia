package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/daemon/authentication/protocol"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/ipc/contract"
)

type fakeProfileApplication struct {
	summaries []config.InstitutionProfileSummary
	err       error
	calls     int
}

func (fake *fakeProfileApplication) ListInstitutionProfiles(context.Context) ([]config.InstitutionProfileSummary, error) {
	fake.calls++
	if fake.err != nil {
		return nil, fake.err
	}
	return append([]config.InstitutionProfileSummary(nil), fake.summaries...), nil
}

func TestProfileHandlerListsSafeSummaries(t *testing.T) {
	fake := &fakeProfileApplication{summaries: []config.InstitutionProfileSummary{{
		InstitutionProfileID:     "jlu",
		DisplayName:              "吉林大学",
		AuthenticationProtocolID: protocol.AuthenticationProtocolID("drcom-5.2.0-d"),
	}}}
	result, cerr := ProfileHandler(fake)(context.Background(), contract.MethodProfileList, []byte(`{}`))
	if cerr != nil {
		t.Fatalf("ProfileHandler error = %+v", cerr)
	}
	if fake.calls != 1 {
		t.Fatalf("ListInstitutionProfiles calls = %d, want 1", fake.calls)
	}
	var decoded contract.ProfileListResult
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal Profile list: %v", err)
	}
	if len(decoded.Profiles) != 1 ||
		decoded.Profiles[0].InstitutionProfileID != "jlu" ||
		decoded.Profiles[0].DisplayName != "吉林大学" ||
		decoded.Profiles[0].AuthenticationProtocolID != "drcom-5.2.0-d" {
		t.Fatalf("Profile list = %#v", decoded.Profiles)
	}
	for _, secret := range []string{"password", "credentialId", "institutionProtocolConfiguration"} {
		if strings.Contains(string(result), secret) {
			t.Errorf("Profile result contains %q: %s", secret, result)
		}
	}
}

func TestProfileHandlerPreservesEmptyArray(t *testing.T) {
	result, cerr := ProfileHandler(&fakeProfileApplication{})(
		context.Background(),
		contract.MethodProfileList,
		[]byte(`{}`),
	)
	if cerr != nil {
		t.Fatalf("ProfileHandler error = %+v", cerr)
	}
	if string(result) != `{"profiles":[]}` {
		t.Errorf("empty Profile list = %s", result)
	}
}

func TestProfileHandlerRejectsMalformedPayloadWithoutCall(t *testing.T) {
	fake := &fakeProfileApplication{}
	_, cerr := ProfileHandler(fake)(
		context.Background(),
		contract.MethodProfileList,
		[]byte(`{"marker":"SECRET-MARKER"}`),
	)
	if cerr == nil || cerr.Code != contract.ErrorCodeInvalidArgument {
		t.Fatalf("malformed payload error = %+v", cerr)
	}
	if fake.calls != 0 {
		t.Errorf("ListInstitutionProfiles calls = %d, want 0", fake.calls)
	}
	if strings.Contains(cerr.Message, "SECRET-MARKER") {
		t.Error("Profile error leaked payload marker")
	}
}

func TestProfileHandlerReturnsStaticFailure(t *testing.T) {
	fake := &fakeProfileApplication{err: errors.New("SECRET-DIAGNOSTIC")}
	_, cerr := ProfileHandler(fake)(context.Background(), contract.MethodProfileList, []byte(`{}`))
	if cerr == nil ||
		cerr.Code != contract.ErrorCodeProfileOperationFailed ||
		cerr.Message != "profile operation failed" {
		t.Fatalf("Profile failure = %+v", cerr)
	}
	if strings.Contains(cerr.Message, "SECRET-DIAGNOSTIC") {
		t.Error("Profile failure leaked diagnostic")
	}
}

func TestProfileHandlerRejectsUnknownMethodWithoutCall(t *testing.T) {
	fake := &fakeProfileApplication{}
	_, cerr := ProfileHandler(fake)(context.Background(), "profile.unknown", []byte(`{}`))
	if cerr == nil || cerr.Code != contract.ErrorCodeUnknownMethod {
		t.Fatalf("unknown method error = %+v", cerr)
	}
	if fake.calls != 0 {
		t.Errorf("ListInstitutionProfiles calls = %d, want 0", fake.calls)
	}
}
