package d520

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
	credential "sidravia/internal/daemon/credentials"
	environment "sidravia/internal/daemon/environment"
)

func buildBinding(t *testing.T, ipv4 string, mac []byte, dns []string, dhcp *string) environment.SelectedSystemNetworkBinding {
	t.Helper()
	assignments := []environment.IPv4AddressAssignment{{
		Address: netip.MustParseAddr(ipv4), PrefixLength: 8,
	}}
	var dnsServers []netip.Addr
	for _, d := range dns {
		dnsServers = append(dnsServers, netip.MustParseAddr(d))
	}
	var dhcpAddr *netip.Addr
	if dhcp != nil {
		a := netip.MustParseAddr(*dhcp)
		dhcpAddr = &a
	}
	networkInterface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{
		InterfaceID:             "test",
		DisplayName:             "test",
		OperationalState:        environment.OperationalStateUp,
		PhysicalMedium:          environment.PhysicalMediumWired,
		AddressAssignmentMethod: environment.AddressAssignmentDHCP,
		HardwareAddress:         mac,
		IPv4AddressAssignments:  assignments,
		DNSServerAddresses:      dnsServers,
		DHCPServerIPv4Address:   dhcpAddr,
	})
	if err != nil {
		t.Fatalf("new network interface: %v", err)
	}
	binding, err := environment.NewSelectedSystemNetworkBinding(networkInterface, environment.IPv4AddressAssignment{Address: netip.MustParseAddr(ipv4), PrefixLength: 8})
	if err != nil {
		t.Fatalf("new binding: %v", err)
	}
	return binding
}

func factoryInputs(config protocol.InstitutionProtocolConfiguration, cred credential.AuthenticationCredential, binding environment.SelectedSystemNetworkBinding) protocol.AuthenticationProtocolRunCreationInputs {
	return protocol.AuthenticationProtocolRunCreationInputs{
		InstitutionProtocolConfiguration: config,
		AuthenticationCredential:         cred,
		SelectedSystemNetworkBinding:     binding,
		SystemHostInformation:            testHostInformation(),
		ProtocolContextOverride:          protocol.AuthenticationProtocolContextOverride(`{}`),
	}
}

func TestFactoryCreatesRunWithValidInputs(t *testing.T) {
	factory := NewFactory()
	if factory.ProtocolID() != ProtocolID {
		t.Fatalf("ProtocolID = %q, want %q", factory.ProtocolID(), ProtocolID)
	}
	run, err := factory.CreateAuthenticationProtocolRun(factoryInputs(validConfig(t), testCredential(), testBinding(t)))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run == nil {
		t.Fatal("run is nil")
	}
}

func TestFactoryAcceptsEmptyPassword(t *testing.T) {
	cred := testCredential()
	cred.Password = ""
	factory := NewFactory()
	if _, err := factory.CreateAuthenticationProtocolRun(factoryInputs(validConfig(t), cred, testBinding(t))); err != nil {
		t.Fatalf("empty password should be accepted: %v", err)
	}
}

// TestFactoryCreatesRunWithChineseHostName proves a valid GBK-encodable
// Chinese Windows host name passes the factory boundary and can create a Run.
// Username and password fixtures remain fictional ASCII.
func TestFactoryCreatesRunWithChineseHostName(t *testing.T) {
	factory := NewFactory()
	inputs := factoryInputs(validConfig(t), testCredential(), testBinding(t))
	inputs.SystemHostInformation.HostName = "校园终端"
	run, err := factory.CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatalf("create run with Chinese host name: %v", err)
	}
	if run == nil {
		t.Fatal("run is nil")
	}
}

func TestFactoryRejectsInvalidCredentials(t *testing.T) {
	cases := []struct {
		name string
		cred credential.AuthenticationCredential
	}{
		{"empty username", credential.AuthenticationCredential{Username: "", Password: "p"}},
		{"username GBK cannot represent", credential.AuthenticationCredential{Username: "user😀", Password: "p"}},
		{"embedded NUL username", credential.AuthenticationCredential{Username: "u\x00ser", Password: "p"}},
		{"embedded NUL password", credential.AuthenticationCredential{Username: "user", Password: "p\x00ass"}},
		{"non-ascii host via overlength", credential.AuthenticationCredential{Username: strings.Repeat("a", 37), Password: "p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewFactory()
			_, err := factory.CreateAuthenticationProtocolRun(factoryInputs(validConfig(t), tc.cred, testBinding(t)))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if strings.Contains(err.Error(), "local-test-password") {
				t.Fatalf("error leaked credential material: %v", err)
			}
		})
	}
}

func TestFactoryRejectsInvalidBinding(t *testing.T) {
	mac := []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	cases := []struct {
		name    string
		binding environment.SelectedSystemNetworkBinding
	}{
		{"unspecified client ipv4", buildBinding(t, "0.0.0.0", mac, nil, nil)},
		{"short mac", buildBinding(t, "127.0.0.1", []byte{0x02, 0x00, 0x00}, nil, nil)},
		{"all-zero mac", buildBinding(t, "127.0.0.1", []byte{0, 0, 0, 0, 0, 0}, nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewFactory()
			_, err := factory.CreateAuthenticationProtocolRun(factoryInputs(validConfig(t), testCredential(), tc.binding))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestFactorySelectDNSZeroFills(t *testing.T) {
	cases := []struct {
		name        string
		dns         []string
		wantPrimary [4]byte
		wantSecond  [4]byte
	}{
		{"two dns", []string{"8.8.8.8", "1.1.1.1"}, [4]byte{8, 8, 8, 8}, [4]byte{1, 1, 1, 1}},
		{"one dns", []string{"8.8.8.8"}, [4]byte{8, 8, 8, 8}, [4]byte{}},
		{"no dns", nil, [4]byte{}, [4]byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binding := buildBinding(t, "127.0.0.1", []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}, tc.dns, nil)
			primary, secondary := selectDNS(binding)
			if primary != tc.wantPrimary {
				t.Fatalf("primary DNS = %x, want %x", primary, tc.wantPrimary)
			}
			if secondary != tc.wantSecond {
				t.Fatalf("secondary DNS = %x, want %x", secondary, tc.wantSecond)
			}
		})
	}
}

func TestFactorySelectDHCP(t *testing.T) {
	mac := []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	dhcp := "10.0.0.1"
	binding := buildBinding(t, "127.0.0.1", mac, nil, &dhcp)
	if got := selectDHCP(binding); got != ([4]byte{10, 0, 0, 1}) {
		t.Fatalf("dhcp = %x, want 0a000001", got)
	}
	bindingNoDHCP := buildBinding(t, "127.0.0.1", mac, nil, nil)
	if got := selectDHCP(bindingNoDHCP); got != ([4]byte{}) {
		t.Fatalf("dhcp = %x, want zero when absent", got)
	}
}

func TestFactoryDeriveHostOS(t *testing.T) {
	cases := []struct {
		name string
		host environment.SystemHostInformation
		want string
	}{
		{"family and release", environment.SystemHostInformation{OperatingSystemFamily: "Windows", OperatingSystemRelease: "10"}, "Windows 10"},
		{"family only", environment.SystemHostInformation{OperatingSystemFamily: "Windows", OperatingSystemRelease: ""}, "Windows"},
		{"release only", environment.SystemHostInformation{OperatingSystemFamily: "", OperatingSystemRelease: "10"}, "10"},
		{"both empty", environment.SystemHostInformation{}, ""},
		{"trimmed", environment.SystemHostInformation{OperatingSystemFamily: " Windows ", OperatingSystemRelease: " 10 "}, "Windows 10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveHostOS(tc.host); got != tc.want {
				t.Fatalf("deriveHostOS = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFactoryCallerMutationSafety(t *testing.T) {
	peer := newTestPeer(t, defaultPeerResponder())
	factory := NewFactory()
	inputs := factoryInputs(testConfigJSON(peer.port(), defaultTestDurations()), testCredential(), testBinding(t))
	run, err := factory.CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	// Mutate every caller-owned mutable slice and field after creation. The run
	// must not be affected because it retained no caller-owned mutable state.
	if len(inputs.InstitutionProtocolConfiguration) > 0 {
		inputs.InstitutionProtocolConfiguration[0] = 0
	}
	inputs.InstitutionProtocolConfiguration = append(inputs.InstitutionProtocolConfiguration, 'X')
	inputs.AuthenticationCredential.Password = "mutated"
	inputs.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(`{"network":"mutated"}`)

	observer := newRecordingObserver(peer)
	_, cancel, done := startRun(t, run, observer)
	defer cancel(context.Canceled)
	observer.waitForEstablished(t, time.Second)
	cancel(context.Canceled)
	assertRunReturns(t, done, true, time.Second)
	if observer.establishedCallCount() != 1 {
		t.Fatalf("established called %d times, want 1 after input mutation", observer.establishedCallCount())
	}
}

func TestFactoryValidateDelegates(t *testing.T) {
	factory := NewFactory()
	if err := factory.ValidateInstitutionProtocolConfiguration(validConfig(t)); err != nil {
		t.Fatalf("validate valid config: %v", err)
	}
	if err := factory.ValidateInstitutionProtocolConfiguration(protocol.InstitutionProtocolConfiguration("null")); err == nil {
		t.Fatal("validate null config expected error")
	}
	if err := factory.ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride(`{}`)); err != nil {
		t.Fatalf("validate empty override: %v", err)
	}
	if err := factory.ValidateProtocolContextOverride(protocol.AuthenticationProtocolContextOverride(`{"a":1}`)); err == nil {
		t.Fatal("validate non-empty override expected error")
	}
}

// TestFactoryInjectsDiagnosticsIntoRun proves the factory carries the supplied
// diagnostics sink into the created run.
func TestFactoryInjectsDiagnosticsIntoRun(t *testing.T) {
	sink := &captureDiagnostics{}
	inputs := factoryInputs(validConfig(t), testCredential(), testBinding(t))
	inputs.Diagnostics = sink
	run, err := NewFactory().CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	d520, ok := run.(*d520Run)
	if !ok {
		t.Fatalf("run type = %T, want *d520Run", run)
	}
	if d520.diagnostics != sink {
		t.Error("diagnostics not injected into run")
	}
}

// TestFactoryUsesNoopWhenDiagnosticsNil proves a nil sink is replaced by the
// explicit no-op implementation rather than a scattered nil check.
func TestFactoryUsesNoopWhenDiagnosticsNil(t *testing.T) {
	inputs := factoryInputs(validConfig(t), testCredential(), testBinding(t))
	inputs.Diagnostics = nil
	run, err := NewFactory().CreateAuthenticationProtocolRun(inputs)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	d520, ok := run.(*d520Run)
	if !ok {
		t.Fatalf("run type = %T, want *d520Run", run)
	}
	if d520.diagnostics == nil {
		t.Error("diagnostics is nil, want Noop implementation")
	}
}
