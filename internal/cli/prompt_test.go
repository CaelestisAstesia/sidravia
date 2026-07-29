package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"sidravia/internal/ipc/contract"
)

func TestReadConfirmationDefaultsNo(t *testing.T) {
	var output bytes.Buffer
	ok, err := readConfirmation(strings.NewReader("\n"), &output, "确认？")
	if err != nil || ok || output.String() != "确认？" {
		t.Fatalf("result = %v %v %q", ok, err, output.String())
	}
}

func TestReadConfirmationAcceptsExplicitAnswersAndRejectsInvalidOrEOF(t *testing.T) {
	for _, test := range []struct {
		input string
		want  bool
	}{
		{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"No\n", false}, {"\n", false},
	} {
		got, err := readConfirmation(strings.NewReader(test.input), io.Discard, "确认？")
		if err != nil || got != test.want {
			t.Fatalf("input %q = %t, %v", test.input, got, err)
		}
	}
	if _, err := readConfirmation(strings.NewReader("maybe\n"), io.Discard, "确认？"); err == nil {
		t.Fatal("invalid confirmation accepted")
	}
	cause := errors.New("read failed")
	if _, err := readConfirmation(errorReader{err: cause}, io.Discard, "确认？"); !errors.Is(err, cause) {
		t.Fatalf("read cause lost: %v", err)
	}
}

type promptClient struct{ response contract.Response }

func (client promptClient) Call(context.Context, string, json.RawMessage) (contract.Response, error) {
	return client.response, nil
}
func (promptClient) Close() error { return nil }

func TestSelectProfileUsesOneBasedValidatedSelection(t *testing.T) {
	raw, err := contract.MarshalProfileListResult(contract.ProfileListResult{Profiles: []contract.ProfileSummaryResult{
		{InstitutionProfileID: "jlu", DisplayName: "吉林大学", AuthenticationProtocolID: "drcom"},
		{InstitutionProfileID: "other", DisplayName: "Other", AuthenticationProtocolID: "other"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	deps := authDependencies{stdin: strings.NewReader("2\n"), stderr: &bytes.Buffer{}}
	got, err := selectProfile(deps, promptClient{response: contract.NewSuccessResponse("1", raw)})
	if err != nil || got != "other" {
		t.Fatalf("selection = %q, %v", got, err)
	}
	deps.stdin = strings.NewReader("0\n")
	if _, err := selectProfile(deps, promptClient{response: contract.NewSuccessResponse("1", raw)}); err == nil {
		t.Fatal("out-of-range profile accepted")
	}
}
