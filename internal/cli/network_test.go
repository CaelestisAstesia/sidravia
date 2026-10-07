package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

func TestNetworkInterfacesUsesOnlyExistingConnectionAndSafePresentation(t *testing.T) {
	const id = "exact'$`id"
	observed := "2026-10-07T01:02:03Z"
	wire, err := contract.MarshalNetworkInterfacesResult(contract.NetworkInterfacesResult{Available: true, Revision: 2, ObservedAt: &observed, Interfaces: []contract.NetworkInterfaceResult{{InterfaceID: id, DisplayName: "Friendly\n\x1b[31m", OperationalState: "up", PhysicalMedium: "unknown", AddressAssignmentMethod: "unknown", IPv4Assignments: []contract.NetworkIPv4Assignment{{Address: "127.0.0.1", PrefixLength: 8, ExplicitBindable: true}}}, {InterfaceID: "down", DisplayName: "", OperationalState: "down", PhysicalMedium: "wired", AddressAssignmentMethod: "static", IPv4Assignments: []contract.NetworkIPv4Assignment{{Address: "192.0.2.1", PrefixLength: 24}}}}})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		if method != contract.MethodNetworkInterfaces || string(payload) != `{}` {
			t.Fatalf("unexpected call %s %s", method, payload)
		}
		return contract.NewSuccessResponse("1", wire), nil
	}}
	var output bytes.Buffer
	deps := hotListDependencies(t, client, &output)
	deps.connection.callTimeout = time.Second
	if err := runNetworkInterfaces(deps); err != nil {
		t.Fatal(err)
	}
	if client.callCount != 1 || client.closeCount != 1 {
		t.Fatal("query did not finish single connection")
	}
	for _, want := range []string{"名称：Friendly��[31m", "ID：" + id, "IPv4：127.0.0.1/8 | 仅可显式绑定", "--interface-id 'exact''$`id' --local-ipv4 '127.0.0.1'", "接口已 Down，当前不可绑定", "使用时会重新核对绑定"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in %q", want, output.String())
		}
	}
	if strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "--interface-id 'down'") {
		t.Fatal("unsafe or unbindable parameters rendered")
	}
}
func TestNetworkInterfacesUnavailableAndReadFailures(t *testing.T) {
	var output bytes.Buffer
	client := &fakeDaemonClient{call: func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewSuccessResponse("1", json.RawMessage(`{"available":false,"revision":0,"interfaces":[]}`)), nil
	}}
	deps := hotListDependencies(t, client, &output)
	deps.connection.callTimeout = time.Second
	if err := runNetworkInterfaces(deps); err != nil || !strings.Contains(output.String(), "尚无已接受") {
		t.Fatal("unavailable observation falsely represented")
	}
	cause := errors.New("private-cause")
	deps.connection.acquire = func(context.Context) (daemonClient, error) { return nil, cause }
	if err := runNetworkInterfaces(deps); !errors.Is(err, cause) {
		t.Fatal("connection cause lost")
	}
	client.call = func(string, json.RawMessage) (contract.Response, error) { return contract.Response{}, cause }
	deps = hotListDependencies(t, client, &output)
	deps.connection.callTimeout = time.Second
	if err := runNetworkInterfaces(deps); !errors.Is(err, cause) || strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("call cause/safe error violated")
	}
	deps.stdout = failingOutputWriter{}
	client.call = func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewSuccessResponse("1", json.RawMessage(`{"available":false,"revision":0,"interfaces":[]}`)), nil
	}
	if err := runNetworkInterfaces(deps); err == nil {
		t.Fatal("write failure swallowed")
	}
}

type failingOutputWriter struct{}

func (failingOutputWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestNetworkDiagnoseUsesExistingConnectionStrictResponseAndSafeLabels(t *testing.T) {
	reason := "protocol"
	wire, err := contract.MarshalNetworkDiagnoseResult(contract.NetworkDiagnoseResult{ObservedAt: "2026-10-07T01:02:03Z", SelectionBasis: "os_route_proposal", Status: "unsupported", UnsupportedReason: &reason, ProtocolSocket: &contract.NetworkProtocolSocket{State: "not_observed", RunGeneration: 0}})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeDaemonClient{call: func(method string, payload json.RawMessage) (contract.Response, error) {
		value, err := contract.DecodeNetworkDiagnosePayload(payload)
		if method != contract.MethodNetworkDiagnose || err != nil || value.ConfigurationID != "configuration-1" || !value.Probe {
			t.Fatal("unexpected readonly request")
		}
		return contract.NewSuccessResponse("1", wire), nil
	}}
	var output bytes.Buffer
	deps := hotListDependencies(t, client, &output)
	deps.connection.callTimeout = time.Second
	if err := runNetworkDiagnose(deps, contract.NetworkDiagnosePayload{ConfigurationID: "configuration-1", Probe: true}); err != nil {
		t.Fatal(err)
	}
	if client.callCount != 1 || client.closeCount != 1 {
		t.Fatal("readonly connection not finalized")
	}
	for _, text := range []string{"完成时间", "OS 路由提案", "最近实际协议 socket", "Run generation 0", "不证明认证成功或 Internet 可用"} {
		if !strings.Contains(output.String(), text) {
			t.Fatal("diagnosis attribution omitted")
		}
	}
	before := client.callCount
	if err := runNetworkDiagnose(deps, contract.NetworkDiagnosePayload{}); err == nil || client.callCount != before {
		t.Fatal("invalid selector acquired connection")
	}
	client.call = func(string, json.RawMessage) (contract.Response, error) {
		return contract.NewSuccessResponse("1", json.RawMessage(`{"status":"available"}`)), nil
	}
	if err := runNetworkDiagnose(deps, contract.NetworkDiagnosePayload{SessionID: "s"}); err == nil {
		t.Fatal("unvalidated response rendered")
	}
}
