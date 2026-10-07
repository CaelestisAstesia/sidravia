package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
)

type diagnosticSpyStore struct {
	*controlledAppStore
	reads, writes atomic.Int32
	protection    jsonfile.ProtectionStatus
}

func (s *diagnosticSpyStore) Read(ctx context.Context, path string, limit int64) ([]byte, bool, error) {
	s.reads.Add(1)
	return s.controlledAppStore.Read(ctx, path, limit)
}
func (s *diagnosticSpyStore) Replace(ctx context.Context, path string, data []byte) error {
	s.writes.Add(1)
	return s.controlledAppStore.Replace(ctx, path, data)
}
func (s *diagnosticSpyStore) ReplaceSensitive(ctx context.Context, path string, data []byte, limit int64, insecure bool) error {
	s.writes.Add(1)
	return s.controlledAppStore.ReplaceSensitive(ctx, path, data, limit, insecure)
}
func (s *diagnosticSpyStore) ProtectionStatus() jsonfile.ProtectionStatus { return s.protection }

type diagnosticSpyFactory struct {
	appTestProtocolFactory
	creates atomic.Int32
}

func (f *diagnosticSpyFactory) CreateAuthenticationProtocolRun(protocol.AuthenticationProtocolRunCreationInputs) (protocol.AuthenticationProtocolRun, error) {
	f.creates.Add(1)
	return nil, errors.New("private-cause-endpoint-password")
}

func TestExportDiagnosticsRealOwnersPrivateAndReadOnly(t *testing.T) {
	ctx := context.Background()
	store := &diagnosticSpyStore{controlledAppStore: newControlledAppStore(), protection: jsonfile.ProtectionProtected}
	catalog, err := config.OpenCatalog(ctx, store, filepath.Join(t.TempDir(), "private-storage-path.json"))
	if err != nil {
		t.Fatal(err)
	}
	value := appTestConfiguration("private-config-id")
	value.DisplayName = "private-display"
	value.Username = "private-account"
	value.InstitutionProfileID = "private-missing-profile"
	value.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(`{"private-override":"private-endpoint"}`)
	value.AutoLogin = true
	value.AutoReconnect = true
	if _, err = catalog.Create(ctx, value, "private-password", false); err != nil {
		t.Fatal(err)
	}
	profile := appTestProfile("private-profile")
	profile.DisplayName = "private-institution"
	profile.InstitutionProtocolConfiguration = protocol.InstitutionProtocolConfiguration(`{"private-profile-configuration":"private-profile-endpoint"}`)
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	factory := &diagnosticSpyFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	host := appTestHostInfo()
	host.HostName = "private-host"
	resolver, err := NewAuthenticationResolver(catalog, profiles, registry, host)
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(appSupervisorDeps())
	defer func() { _ = sup.Close(); sup.Wait() }()
	application, err := NewApplication(catalog, profiles, resolver, sup)
	if err != nil {
		t.Fatal(err)
	}
	definition := session.RuntimeDefinition{Configuration: session.Configuration{ConfigurationID: value.ConfigurationID, DisplayName: "private-session-display", InstitutionProfileID: profile.InstitutionProfileID, NetworkBindingPolicy: session.NetworkBindingPolicy{Mode: session.AutomaticallySelectLatestAvailable}, ProtocolContextOverride: value.ProtocolContextOverride}, InstitutionProfile: profile, AuthenticationCredential: credential.AuthenticationCredential{Username: "private-account", Password: "private-password"}, AuthenticationProtocolFactory: factory, SystemHostInformation: host}
	for i := 0; i < 65; i++ {
		if _, _, err := sup.StartResolved(ctx, definition, session.SuspendAuthentication); err != nil {
			t.Fatal(err)
		}
	}
	iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{InterfaceID: "private-interface-id", DisplayName: "private-interface-name", HardwareAddress: []byte{1, 2, 3, 4, 5, 6}, DNSServerAddresses: []netip.Addr{netip.MustParseAddr("203.0.113.53")}, IPv4AddressAssignments: []environment.IPv4AddressAssignment{{Address: netip.MustParseAddr("192.0.2.99"), PrefixLength: 24}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.ApplySystemNetworkSnapshot(ctx, environment.NewSnapshot(99, time.Now(), []environment.NetworkInterface{iface})); err != nil {
		t.Fatal(err)
	}
	before, err := sup.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	application.opMu.Lock()
	application.invalidSessions[before[0].AuthenticationSessionID] = true
	application.opMu.Unlock()
	readCount, writeCount := store.reads.Load(), store.writes.Load()
	result, err := application.ExportDiagnostics(ctx, "1.0.0+test", "build-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := contract.MarshalDiagnosticsExportResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sessions.TotalCount != 65 || !result.Sessions.Truncated || len(result.Sessions.Items) != 64 {
		t.Fatalf("incorrect session bound: %#v", result.Sessions)
	}
	if !result.Network.Available || *result.Network.InterfaceCount != 1 || *result.Network.IPv4AssignmentCount != 1 || result.Catalog.TotalConfigurations != 1 || result.Catalog.AutoLoginConfigurations != 1 || result.Catalog.AutoReconnectConfigurations != 1 {
		t.Fatalf("incorrect owner counts: %#v", result)
	}
	if result.Catalog.ProfileUnavailableConfigurations != 1 || result.Catalog.AvailableConfigurations != 0 || *result.Network.UpInterfaceCount != 0 || *result.Network.AutomaticCandidateCount != 0 || *result.Network.ExplicitBindableCount != 0 {
		t.Fatal("useful blockers/candidate facts lost")
	}
	for i, item := range result.Sessions.Items {
		if item.State != "suspended" || item.Intent != "suspend_authentication" || item.ProtocolSocketState != "not_observed" || item.FailureCategory != "none" || item.RecoveryRecommendation != "none" || item.CleanupRequired != (i == 0) {
			t.Fatalf("invented actor facts: %#v", item)
		}
	}
	for _, marker := range []string{"private-", "192.0.2.99", "203.0.113.53", "session-", "drcom", "password", `"private-override"`, `{"private-override":"private-endpoint"}`, "hostName", "interfaceId", "configurationId", "runGeneration", "reasonDescription", "localEndpoint"} {
		if strings.Contains(string(raw), marker) {
			t.Fatalf("private marker %q in safe artifact", marker)
		}
	}
	after, err := sup.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || factory.creates.Load() != 0 || store.reads.Load() != readCount || store.writes.Load() != writeCount {
		t.Fatal("export changed actors, invoked authentication or accessed persisted secrets")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 9 || len(raw) > 32768 {
		t.Fatal("artifact allowlist or byte limit changed")
	}
}

func TestExportDiagnosticsContextsDependenciesAndNormalization(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	if _, err := setup.application.ExportDiagnostics(nil, "v", "b"); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("private-cause-token")
	cancel(cause)
	if _, err := setup.application.ExportDiagnostics(ctx, "v", "b"); !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel cause lost: %v", err)
	}
	value, err := setup.application.ExportDiagnostics(context.Background(), "private/path", "secret\n")
	if err != nil {
		t.Fatal(err)
	}
	if value.ProductVersion != "unknown" || value.BuildID != "unknown" || value.Network.Available || value.Network.InterfaceCount != nil || value.Network.IPv4AssignmentCount != nil {
		t.Fatal("invalid metadata or unavailable counts fabricated")
	}
	_ = setup.supervisor.Close()
	setup.supervisor.Wait()
	if _, err := setup.application.ExportDiagnostics(context.Background(), "v", "b"); err == nil {
		t.Fatal("closed owner exported fake zero observations")
	}
	handler := DiagnosticsExportHandler(setup.application, "v", "b")
	_, publicErr := handler(context.Background(), contract.MethodDiagnosticsExport, []byte(`{}`))
	if publicErr == nil || publicErr.Code != contract.ErrorCodeInternalError || publicErr.Message != "diagnostic export failed" {
		t.Fatalf("unsafe dependency error: %#v", publicErr)
	}
}

func TestDiagnosticProjectionFutureCategoriesAndCounterGuards(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	paired := session.NetworkDiagnosticsSnapshot{Snapshot: session.Snapshot{State: "private-future-state", Intent: "private-future-intent", StateReason: &session.StateReason{Code: "private-future-code", Description: "private-secret-cause"}, LastAuthenticationFailure: &session.AuthenticationFailure{Description: "private-cause-chain"}}, ProtocolSocket: session.ProtocolSocketObservation{State: "private-future-socket"}}
	item := diagnosticSession(paired, true)
	if item.State != "other" || item.Intent != "other" || item.ReasonCode != "other" || item.ProtocolSocketState != "other" || item.FailureCategory != "other" || item.RecoveryRecommendation != "other" || !item.CleanupRequired {
		t.Fatal("unknown domain categories not bounded")
	}
	raw, _ := json.Marshal(item)
	if strings.Contains(string(raw), "private") {
		t.Fatal("future cause leaked")
	}
	if _, err := diagnosticCount(1 << 32); err == nil {
		t.Fatal("counter wrapped")
	}
	if got, err := diagnosticCount((1 << 32) - 1); err != nil || got != ^uint32(0) {
		t.Fatal("valid maximum rejected")
	}
	for _, mode := range []config.NetworkBindingPolicyMode{"private-mode"} {
		value := appTestConfiguration("c")
		value.NetworkBindingPolicy.Mode = mode
		if _, err := setup.application.diagnosticCatalog(context.Background(), []config.Configuration{value}, "protected"); err == nil {
			t.Fatal("unknown binding mode fabricated")
		}
	}
	if _, err := setup.application.diagnosticCatalog(context.Background(), nil, "private-protection"); err == nil {
		t.Fatal("unknown protection fabricated")
	}
	for _, protection := range []string{"protected", "unprotected"} {
		if _, err := setup.application.diagnosticCatalog(context.Background(), nil, protection); err != nil {
			t.Fatal(err)
		}
	}
}

// A real registry Factory validates only cloned Override input, never creates a
// Run for catalog availability. No private value enters the exported counts.
type exportAvailabilityFactory struct{ diagnosticSpyFactory }

func (*exportAvailabilityFactory) ValidateProtocolContextOverride(raw protocol.AuthenticationProtocolContextOverride) error {
	if strings.Contains(string(raw), "private-rejected") {
		return errors.New("private-validator-cause")
	}
	return nil
}
func TestExportDiagnosticCatalogAvailabilityUsesExistingOwners(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	ctx := context.Background()
	missing := appTestProfile("missing-factory")
	missing.AuthenticationProtocolID = "private-unregistered"
	profiles, err := config.NewProfileCatalog([]config.InstitutionProfile{appTestProfile("profile-1"), missing})
	if err != nil {
		t.Fatal(err)
	}
	setup.application.profiles, setup.application.authenticationResolver.profiles = profiles, profiles
	factory := &exportAvailabilityFactory{diagnosticSpyFactory: diagnosticSpyFactory{appTestProtocolFactory: appTestProtocolFactory{id: "drcom"}}}
	registry, err := protocol.NewAuthenticationProtocolRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	setup.application.authenticationResolver.protocols = registry
	for _, input := range []struct{ id, profile, override string }{{"private-no-profile", "private-absent", "{}"}, {"private-no-protocol", "missing-factory", "{}"}, {"private-invalid-override", "profile-1", `{"private-rejected":"private-value"}`}} {
		value := appTestConfiguration(config.ConfigurationID(input.id))
		value.InstitutionProfileID = config.InstitutionProfileID(input.profile)
		value.ProtocolContextOverride = protocol.AuthenticationProtocolContextOverride(input.override)
		if _, err := setup.catalog.Create(ctx, value, "private-password", false); err != nil {
			t.Fatal(err)
		}
	}
	setup.store.mu.Lock()
	before := map[string][]byte{}
	for k, v := range setup.store.data {
		before[k] = append([]byte(nil), v...)
	}
	setup.store.mu.Unlock()
	result, err := setup.application.ExportDiagnostics(ctx, "v", "b")
	if err != nil {
		t.Fatal(err)
	}
	c := result.Catalog
	if c.TotalConfigurations != 4 || c.AvailableConfigurations != 1 || c.ProfileUnavailableConfigurations != 1 || c.ProtocolUnavailableConfigurations != 1 || c.OverrideInvalidConfigurations != 1 {
		t.Fatalf("missing startup blockers: %#v", c)
	}
	raw, err := contract.MarshalDiagnosticsExportResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-") {
		t.Fatal("private validation data leaked")
	}
	setup.store.mu.Lock()
	unchanged := reflect.DeepEqual(before, setup.store.data)
	setup.store.mu.Unlock()
	if !unchanged || factory.creates.Load() != 0 {
		t.Fatal("export mutated persistence or created a Run")
	}
}
func TestDiagnosticNetworkCountsKeepAutomaticAndExplicitFacts(t *testing.T) {
	iface, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{InterfaceID: "test-physical", OperationalState: environment.OperationalStateUp, HardwareBacked: true, PhysicalConnectorPresent: true, IPv4AddressAssignments: []environment.IPv4AddressAssignment{{Address: netip.MustParseAddr("127.0.0.1"), PrefixLength: 8}, {Address: netip.MustParseAddr("192.0.2.1"), PrefixLength: 24}}})
	if err != nil {
		t.Fatal(err)
	}
	down, err := environment.NewNetworkInterface(environment.NetworkInterfaceFacts{InterfaceID: "test-down", OperationalState: environment.OperationalStateDown, IPv4AddressAssignments: []environment.IPv4AddressAssignment{{Address: netip.MustParseAddr("192.0.2.2"), PrefixLength: 24}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := diagnosticNetwork(environment.NewSnapshot(1, time.Now(), []environment.NetworkInterface{iface, down}), true)
	if err != nil {
		t.Fatal(err)
	}
	if *result.InterfaceCount != 2 || *result.IPv4AssignmentCount != 3 || *result.UpInterfaceCount != 1 || *result.AutomaticCandidateCount != 1 || *result.ExplicitBindableCount != 2 {
		t.Fatalf("policy facts lost: %#v", result)
	}
	unavailable, err := diagnosticNetwork(environment.Snapshot{}, false)
	if err != nil || unavailable.UpInterfaceCount != nil || unavailable.AutomaticCandidateCount != nil || unavailable.ExplicitBindableCount != nil {
		t.Fatal("unavailable facts fabricated")
	}
}
func TestDiagnosticFailuresGroupPrivateAccountOutcomesAndRetainRecovery(t *testing.T) {
	groups := map[string][]string{
		"authentication_rejected": {"credential_invalid", "session_in_use", "insufficient_funds", "account_frozen", "too_many_sessions", "authentication_rejected"},
		"binding_rejected":        {"binding_ip_mismatch", "binding_mac_mismatch", "binding_pair_mismatch", "dhcp_required"},
		"network_io_failure":      {"network_io_failed"}, "protocol_incompatible": {"incompatible_version"}, "run_creation_failed": {"protocol_run_creation_failed", "run_creation_failed"}, "runtime_definition_unavailable": {"runtime_definition_unavailable"},
		"network_timeout": {"network_timeout"}, "server_busy": {"server_busy"}, "protocol_response_invalid": {"protocol_response_invalid"}, "protocol_contract_violated": {"protocol_contract_violated"}, "logout_cleanup_failed": {"logout_cleanup_failed"}, "other": {"private-future-code"},
	}
	for want, codes := range groups {
		for _, code := range codes {
			paired := session.NetworkDiagnosticsSnapshot{Snapshot: session.Snapshot{LastAuthenticationFailure: &session.AuthenticationFailure{Code: protocol.AuthenticationProtocolFailureCode(code), Description: "private-account-cause", HandlingRecommendation: protocol.BlockUntilExplicitRestartOrRelevantInputChange}}}
			item := diagnosticSession(paired, true)
			if item.FailureCategory != want || item.RecoveryRecommendation != string(protocol.BlockUntilExplicitRestartOrRelevantInputChange) || !item.CleanupRequired {
				t.Fatalf("lost safe failure %s: %#v", code, item)
			}
			raw, _ := json.Marshal(item)
			if strings.Contains(string(raw), "private-") {
				t.Fatal("failure cause leaked")
			}
		}
	}
	for _, recommendation := range []protocol.AuthenticationProtocolFailureHandlingRecommendation{protocol.RetryAfterStandardDelay, protocol.RetryAfterExtendedDelay} {
		paired := session.NetworkDiagnosticsSnapshot{Snapshot: session.Snapshot{LastAuthenticationFailure: &session.AuthenticationFailure{Code: "network_timeout", HandlingRecommendation: recommendation}}}
		if diagnosticSession(paired, false).RecoveryRecommendation != string(recommendation) {
			t.Fatal("original recovery recommendation lost")
		}
	}
}
