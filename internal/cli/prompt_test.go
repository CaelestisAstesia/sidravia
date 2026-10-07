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

func TestReadBooleanPromptDefaultsAndExplicit(t *testing.T) {
	for _, test := range []struct {
		name    string
		input   string
		def     bool
		want    bool
		wantErr bool
	}{
		{"default true empty", "\n", true, true, false},
		{"default false empty", "\n", false, false, false},
		{"explicit yes", "y\n", false, true, false},
		{"explicit no", "n\n", true, false, false},
		{"uppercase yes", "YES\n", false, true, false},
		{"uppercase no", "NO\n", true, false, false},
		{"invalid", "maybe\n", true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := readBooleanPrompt(strings.NewReader(test.input), io.Discard, "提示", test.def)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSelectNetworkBindingDefaultsWhenNoObservedFactsExist(t *testing.T) {
	raw, err := contract.MarshalNetworkInterfacesResult(contract.NetworkInterfacesResult{
		Available: false, Interfaces: []contract.NetworkInterfaceResult{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"\n", "0\n"} {
		deps := authDependencies{stdin: strings.NewReader(selection + "password\n"), stderr: &bytes.Buffer{}}
		policy, err := selectNetworkBinding(deps, promptClient{response: contract.NewSuccessResponse("1", raw)})
		if err != nil || policy.Mode != automaticNetworkBindingPolicy {
			t.Fatalf("selection %q = %#v, %v", selection, policy, err)
		}
		password, err := readPasswordLine(deps.stdin)
		if err != nil || password != "password" {
			t.Fatalf("following password = %q, %v", password, err)
		}
	}
}

func TestSelectNetworkBindingUsesSortedExactBindablePairs(t *testing.T) {
	observedAt := "2026-10-07T00:00:00Z"
	assignment := func(address string, prefix uint8, automatic, explicit bool) contract.NetworkIPv4Assignment {
		return contract.NetworkIPv4Assignment{Address: address, PrefixLength: prefix, AutomaticCandidate: automatic, ExplicitBindable: explicit}
	}
	iface := func(id, name, state string, assignments ...contract.NetworkIPv4Assignment) contract.NetworkInterfaceResult {
		return contract.NetworkInterfaceResult{
			InterfaceID: id, DisplayName: name, OperationalState: state, PhysicalMedium: "unknown",
			AddressAssignmentMethod: "unknown", IPv4Assignments: assignments,
		}
	}
	raw, err := contract.MarshalNetworkInterfacesResult(contract.NetworkInterfacesResult{
		Available: true, Revision: 5, ObservedAt: &observedAt,
		Interfaces: []contract.NetworkInterfaceResult{
			iface("z-software", "Zed", "up", assignment("192.0.2.8", 24, false, true), assignment("127.0.0.1", 32, true, true), assignment("127.0.0.1", 8, false, true)),
			iface("down", "Down", "down", assignment("192.0.2.9", 24, false, true)),
			iface("unbound", "Unbound", "up", assignment("192.0.2.10", 24, false, false)),
			iface("a-loopback", "Loopback", "up", assignment("127.0.0.1", 8, false, true)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	deps := authDependencies{stdin: strings.NewReader("2\npassword\n"), stderr: &output}
	policy, err := selectNetworkBinding(deps, promptClient{response: contract.NewSuccessResponse("1", raw)})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Mode != "explicit_interface_and_local_ipv4" || policy.InterfaceID != "z-software" || policy.LocalIPv4Address != "127.0.0.1" {
		t.Fatalf("selected policy = %#v", policy)
	}
	text := output.String()
	first := strings.Index(text, "1. Loopback | ID：a-loopback | IPv4：127.0.0.1/8")
	second := strings.Index(text, "2. Zed | ID：z-software | IPv4：127.0.0.1/8")
	third := strings.Index(text, "3. Zed | ID：z-software | IPv4：192.0.2.8/24")
	if first < 0 || second <= first || third <= second || strings.Contains(text, "ID：down") || strings.Contains(text, "ID：unbound") || strings.Contains(text, "127.0.0.1/32") {
		t.Fatalf("choices are not filtered, sorted and deduplicated: %q", text)
	}
	password, err := readPasswordLine(deps.stdin)
	if err != nil || password != "password" {
		t.Fatalf("following password = %q, %v", password, err)
	}
}

func TestReadChoiceLineRequiresReceivedNewlineAndRejectsInvalidChoice(t *testing.T) {
	for _, input := range []string{"", "2"} {
		if _, err := readChoiceLine(strings.NewReader(input), io.Discard, "choose: "); !errors.Is(err, io.EOF) {
			t.Fatalf("EOF input %q accepted", input)
		}
	}
	for _, input := range []string{"-1\n", "x\n"} {
		if _, err := readChoiceLine(strings.NewReader(input), io.Discard, "choose: "); err != nil {
			t.Fatalf("line reader rejected complete line %q: %v", input, err)
		}
	}
}
