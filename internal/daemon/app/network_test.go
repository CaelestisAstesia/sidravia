package app

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func TestNetworkQueryProjectsAllFactsAndCurrentEligibility(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	var interfaces []environment.NetworkInterface
	for _, test := range []struct {
		id        string
		state     environment.OperationalState
		hardware  bool
		addresses []string
	}{
		{"z-physical", environment.OperationalStateUp, true, []string{"192.0.2.10", "192.0.2.2", "127.0.0.1"}},
		{"a-loopback", environment.OperationalStateUp, false, []string{"127.0.0.1"}},
		{"d-down", environment.OperationalStateDown, true, []string{"192.0.2.4"}},
		{"c-software", environment.OperationalStateUp, false, []string{"192.0.2.3"}},
		{"b-empty", environment.OperationalStateUp, false, nil},
	} {
		assignments := []environment.IPv4AddressAssignment{}
		for _, address := range test.addresses {
			assignments = append(assignments, environment.IPv4AddressAssignment{Address: netip.MustParseAddr(address), PrefixLength: 24})
		}
		iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{InterfaceID: environment.InterfaceID(test.id), DisplayName: "name", OperationalState: test.state, PhysicalMedium: environment.PhysicalMediumUnknown, HardwareBacked: test.hardware, PhysicalConnectorPresent: test.hardware, AddressAssignmentMethod: environment.AddressAssignmentUnknown, IPv4AddressAssignments: assignments, HardwareAddress: []byte{1, 2, 3, 4, 5, 6}, DNSServerAddresses: []netip.Addr{netip.MustParseAddr("198.51.100.9")}})
		if err != nil {
			t.Fatal(err)
		}
		interfaces = append(interfaces, iface)
	}
	observed := time.Date(2026, 10, 7, 1, 2, 3, 123456789, time.UTC)
	snapshot := environment.NewSnapshot(3, observed, interfaces)
	if err := setup.application.ApplySystemNetworkSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	handler := IPCHandler(setup.application, "v", "b", launchcontract.Headless())
	wire, rpcErr := handler(context.Background(), contract.MethodNetworkInterfaces, json.RawMessage(`{}`))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	result, err := contract.DecodeNetworkInterfacesResult(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || result.Revision != 3 || *result.ObservedAt != observed.Format(time.RFC3339Nano) || len(result.Interfaces) != len(interfaces) {
		t.Fatal("query lost observation facts")
	}
	for i, row := range result.Interfaces {
		if i > 0 && result.Interfaces[i-1].InterfaceID > row.InterfaceID {
			t.Fatal("interface order")
		}
		for j, address := range row.IPv4Assignments {
			if j > 0 && netip.MustParseAddr(row.IPv4Assignments[j-1].Address).Compare(netip.MustParseAddr(address.Address)) > 0 {
				t.Fatal("numeric address order")
			}
			for _, iface := range interfaces {
				if string(iface.InterfaceID) == row.InterfaceID {
					assignment := environment.IPv4AddressAssignment{Address: netip.MustParseAddr(address.Address), PrefixLength: address.PrefixLength}
					if address.AutomaticCandidate != session.IsAutomaticBindingCandidate(iface, assignment) {
						t.Fatal("query differs from selector eligibility")
					}
					expectedExplicit := row.OperationalState == "up"
					if address.ExplicitBindable != expectedExplicit {
						t.Fatal("explicit eligibility differs from current facts")
					}
				}
			}
		}
	}
	physical := result.Interfaces[4].IPv4Assignments
	if physical[0].Address != "127.0.0.1" || physical[0].AutomaticCandidate || !physical[0].ExplicitBindable || physical[1].Address != "192.0.2.2" || !physical[1].AutomaticCandidate {
		t.Fatal("physical loopback/ordering rule")
	}
	for _, forbidden := range []string{"hardwareAddress", "dnsServer", "198.51.100.9", "gateway", "password", "username", "token", "configurationId"} {
		if strings.Contains(string(wire), forbidden) {
			t.Fatalf("query leaked %s", forbidden)
		}
	}
}
func TestNetworkQueryUnavailablePayloadAndSafeFailure(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "v", "b", launchcontract.Headless())
	wire, rpcErr := handler(context.Background(), contract.MethodNetworkInterfaces, []byte(`{}`))
	if rpcErr != nil || string(wire) != `{"available":false,"revision":0,"interfaces":[]}` {
		t.Fatalf("unavailable = %s %#v", wire, rpcErr)
	}
	for _, payload := range []string{`null`, `[]`, `{"x":1}`, `{"interfaces":[]}`, `{} {}`} {
		_, rpcErr := handler(context.Background(), contract.MethodNetworkInterfaces, []byte(payload))
		if rpcErr == nil || rpcErr.Code != contract.ErrorCodeInvalidArgument {
			t.Fatalf("accepted %s", payload)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		_, rpcErr := handler(ctx, contract.MethodNetworkInterfaces, []byte(`{}`))
		if rpcErr == nil || rpcErr.Code != contract.ErrorCodeInternalError || rpcErr.Message != "network query failed" {
			t.Fatalf("failure = %#v", rpcErr)
		}
	}
	_ = setup.supervisor.Close()
	setup.supervisor.Wait()
	_, rpcErr = handler(context.Background(), contract.MethodNetworkInterfaces, []byte(`{}`))
	if rpcErr == nil || rpcErr.Message != "network query failed" {
		t.Fatal("closed read exposed diagnostic")
	}
}
