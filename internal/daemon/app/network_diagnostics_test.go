package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

// Synthetic test Profile inherits the complete official grammar but replaces
// its destination and socket policy. No test authenticates or emits D520 data.
func diagnosticLoopbackProfile(t *testing.T) config.InstitutionProfile {
	t.Helper()
	raw, err := os.ReadFile("../configuration/profiles/jlu.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Configuration map[string]any `json:"institutionProtocolConfiguration"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document.Configuration["serverAddress"] = "127.0.0.1"
	document.Configuration["localPort"] = map[string]any{"mode": "system_assigned"}
	encoded, err := json.Marshal(document.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	return config.InstitutionProfile{InstitutionProfileID: "diagnostic-test", DisplayName: "Owned test", AuthenticationProtocolID: d520.ProtocolID, InstitutionProtocolConfiguration: encoded}
}
func diagnosticApplication(t *testing.T, profile config.InstitutionProfile, factory protocol.AuthenticationProtocolFactory, policy session.NetworkBindingPolicy) *applicationTestSetup {
	t.Helper()
	ctx := context.Background()
	store := newControlledAppStore()
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "configurations.json"))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewAuthenticationResolver(catalog, profiles, registry, appTestHostInfo())
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(appSupervisorDeps())
	application, err := NewApplication(catalog, profiles, resolver, sup)
	if err != nil {
		t.Fatal(err)
	}
	value := appTestConfiguration("configuration-1")
	value.InstitutionProfileID = profile.InstitutionProfileID
	value.NetworkBindingPolicy = policy
	if _, err := catalog.Create(ctx, value, "", false); err != nil {
		t.Fatal(err)
	}
	setup := &applicationTestSetup{application: application, catalog: catalog, supervisor: sup, store: store}
	t.Cleanup(setup.cleanup)
	return setup
}
func TestNetworkDiagnosisConfigurationReadOnlyAndPlatformTruth(t *testing.T) {
	setup := diagnosticApplication(t, diagnosticLoopbackProfile(t), d520.NewFactory(), session.NetworkBindingPolicy{Mode: session.AutomaticallySelectLatestAvailable})
	before, _ := setup.catalog.Get(context.Background(), "configuration-1")
	persisted := map[string][]byte{}
	for k, v := range setup.store.data {
		persisted[k] = bytes.Clone(v)
	}
	result, err := setup.application.DiagnoseNetwork(context.Background(), contract.NetworkDiagnosePayload{ConfigurationID: "configuration-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectionBasis != "os_route_proposal" || result.Target.Address != "127.0.0.1" || result.ProtocolSocket != nil {
		t.Fatal("unstarted configuration facts incorrect")
	}
	if runtime.GOOS != "windows" && (result.Status != "unsupported" || *result.UnsupportedReason != "platform") {
		t.Fatal("platform unsupported not truthful")
	}
	if _, err := contract.MarshalNetworkDiagnoseResult(result); err != nil {
		t.Fatal(err)
	}
	after, _ := setup.catalog.Get(context.Background(), "configuration-1")
	sessions, _ := setup.supervisor.List(context.Background())
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(persisted, setup.store.data) || len(sessions) != 0 {
		t.Fatal("diagnosis changed configuration or created Session")
	}
}
func TestNetworkDiagnosisSelectionAndRetainedSocketAttribution(t *testing.T) {
	target := session.NetworkDiagnosticTarget{Endpoint: netip.MustParseAddrPort("192.0.2.1:61440"), Policy: session.NetworkBindingPolicy{Mode: session.ExplicitInterfaceAndLocalIPv4, InterfaceID: "explicit", LocalIPv4Address: netip.MustParseAddr("192.0.2.2")}, Supported: true}
	query, basis := networkDiagnosticSelection(target, nil, true)
	if basis != "configuration_explicit" || query.InterfaceID != "explicit" || query.SourceIPv4 != target.Policy.LocalIPv4Address || !query.Probe {
		t.Fatal("explicit policy was relaxed")
	}
	actor := session.NetworkDiagnosticsSnapshot{Snapshot: session.Snapshot{SelectedNetworkBinding: &session.NetworkBindingSummary{InterfaceID: "current", LocalIPv4Address: netip.MustParseAddr("192.0.2.3")}}, ProtocolSocket: session.ProtocolSocketObservation{State: session.ProtocolSocketClosed, RunGeneration: 1, UpdatedAt: time.Unix(100, 0), LocalEndpoint: netip.MustParseAddrPort("192.0.2.2:40001"), RemoteEndpoint: netip.MustParseAddrPort("255.255.255.255:61440")}}
	query, basis = networkDiagnosticSelection(target, &actor, false)
	socket := networkProtocolSocket(actor.ProtocolSocket)
	if basis != "session_binding" || query.InterfaceID != "current" || query.SourceIPv4.String() != "192.0.2.3" || socket.LocalEndpoint.Address != "192.0.2.2" || socket.RemoteEndpoint.Address != "255.255.255.255" {
		t.Fatal("current desired binding replaced retained socket")
	}
}
func TestNetworkDiagnosisSessionOneShotAndQuarantine(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	started, err := setup.application.StartOneShotAuthentication(context.Background(), validOneShotInput())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := setup.supervisor.Get(context.Background(), started.SessionID)
	result, err := setup.application.DiagnoseNetwork(context.Background(), contract.NetworkDiagnosePayload{SessionID: string(started.SessionID)})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := setup.supervisor.Get(context.Background(), started.SessionID)
	if result.Status != "unsupported" || *result.UnsupportedReason != "protocol" || result.Target != nil || result.ProtocolSocket.RunGeneration != 0 || !reflect.DeepEqual(before, after) {
		t.Fatal("one-shot read changed runtime or invented target")
	}
	setup.application.opMu.Lock()
	setup.application.invalidSessions[started.SessionID] = true
	setup.application.opMu.Unlock()
	if _, err := setup.application.DiagnoseNetwork(context.Background(), contract.NetworkDiagnosePayload{SessionID: string(started.SessionID)}); !errors.Is(err, supervisor.ErrSessionStateConflict) {
		t.Fatal("quarantined Session accepted")
	}
}
func TestNetworkDiagnosisMissingAssociatedSessionNotHidden(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	setup.application.opMu.Lock()
	setup.application.mu.Lock()
	setup.application.sessionsByConfig["configuration-1"] = "missing"
	setup.application.mu.Unlock()
	setup.application.opMu.Unlock()
	if _, err := setup.application.DiagnoseNetwork(context.Background(), contract.NetworkDiagnosePayload{ConfigurationID: "configuration-1"}); !errors.Is(err, supervisor.ErrSessionNotFound) {
		t.Fatal("missing associated Session hidden")
	}
}
func TestNetworkDiagnosisBroadcastClassifiedBeforeOS(t *testing.T) {
	profile := diagnosticLoopbackProfile(t)
	profile.InstitutionProtocolConfiguration = bytes.Replace(profile.InstitutionProtocolConfiguration, []byte("127.0.0.1"), []byte("255.255.255.255"), 1)
	setup := diagnosticApplication(t, profile, d520.NewFactory(), session.NetworkBindingPolicy{Mode: session.AutomaticallySelectLatestAvailable})
	result, err := setup.application.DiagnoseNetwork(context.Background(), contract.NetworkDiagnosePayload{ConfigurationID: "configuration-1", Probe: true})
	if err != nil || result.Status != "unsupported" || *result.UnsupportedReason != "destination" || result.Target.Address != "255.255.255.255" || result.Route != nil || result.Probe != nil {
		t.Fatal("broadcast should retain grammar and skip OS")
	}
}
func TestNetworkDiagnosisIPCSelectorsAndSafeErrors(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := IPCHandler(setup.application, "v", "b", launchcontract.Headless())
	for _, test := range []struct{ payload, code string }{
		{`{}`, contract.ErrorCodeInvalidArgument}, {`{"configurationId":"configuration-1","sessionId":"s"}`, contract.ErrorCodeInvalidArgument},
		{`{"configurationId":"missing"}`, contract.ErrorCodeConfigurationNotFound}, {`{"sessionId":"missing"}`, contract.ErrorCodeSessionNotFound},
	} {
		_, rpc := handler(context.Background(), contract.MethodNetworkDiagnose, []byte(test.payload))
		if rpc == nil || rpc.Code != test.code || strings.Contains(rpc.Message, "missing\"") {
			t.Fatal("unsafe or incorrect error classification")
		}
	}
	raw, rpc := handler(context.Background(), contract.MethodNetworkDiagnose, []byte(`{"configurationId":"configuration-1"}`))
	if rpc != nil {
		t.Fatal(rpc)
	}
	if _, err := contract.DecodeNetworkDiagnoseResult(raw); err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"configurationId", "sessionId", "accountName", "institution", "password", "username", "protocolContextOverride"} {
		if strings.Contains(string(raw), word) {
			t.Fatal("identity or secret metadata in network projection")
		}
	}
}
