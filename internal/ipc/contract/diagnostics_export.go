package contract

import (
	"encoding/json"
	"errors"
)

const MethodDiagnosticsExport = "diagnostics.export"
const MaximumDiagnosticExportBytes = 32768
const MaximumDiagnosticSessionItems = 64

// These DTOs are the entire curated artifact allowlist. They carry counts and
// fixed categories, never source identities, endpoints, descriptions or causes.
type DiagnosticsExportResult struct {
	SchemaVersion   uint32                   `json:"schemaVersion"`
	GeneratedAt     string                   `json:"generatedAt"`
	ProductVersion  string                   `json:"productVersion"`
	BuildID         string                   `json:"buildId"`
	OperatingSystem string                   `json:"operatingSystem"`
	Architecture    string                   `json:"architecture"`
	Network         DiagnosticExportNetwork  `json:"network"`
	Catalog         DiagnosticExportCatalog  `json:"catalog"`
	Sessions        DiagnosticExportSessions `json:"sessions"`
}
type DiagnosticExportNetwork struct {
	Available               bool    `json:"available"`
	InterfaceCount          *uint32 `json:"interfaceCount,omitempty"`
	IPv4AssignmentCount     *uint32 `json:"ipv4AssignmentCount,omitempty"`
	UpInterfaceCount        *uint32 `json:"upInterfaceCount,omitempty"`
	AutomaticCandidateCount *uint32 `json:"automaticCandidateCount,omitempty"`
	ExplicitBindableCount   *uint32 `json:"explicitBindableCount,omitempty"`
}
type DiagnosticExportCatalog struct {
	AvailableConfigurations           uint32 `json:"availableConfigurations"`
	ProfileUnavailableConfigurations  uint32 `json:"profileUnavailableConfigurations"`
	ProtocolUnavailableConfigurations uint32 `json:"protocolUnavailableConfigurations"`
	OverrideInvalidConfigurations     uint32 `json:"overrideInvalidConfigurations"`
	StorageProtection                 string `json:"storageProtection"`
	TotalConfigurations               uint32 `json:"totalConfigurations"`
	AutoLoginConfigurations           uint32 `json:"autoLoginConfigurations"`
	AutoReconnectConfigurations       uint32 `json:"autoReconnectConfigurations"`
	AutomaticBindingConfigurations    uint32 `json:"automaticBindingConfigurations"`
	ExplicitBindingConfigurations     uint32 `json:"explicitBindingConfigurations"`
}
type DiagnosticExportSessions struct {
	TotalCount uint32                    `json:"totalCount"`
	Truncated  bool                      `json:"truncated"`
	Items      []DiagnosticExportSession `json:"items"`
}
type DiagnosticExportSession struct {
	FailureCategory        string `json:"failureCategory"`
	RecoveryRecommendation string `json:"recoveryRecommendation"`
	CleanupRequired        bool   `json:"cleanupRequired"`
	State                  string `json:"state"`
	Intent                 string `json:"intent"`
	ReasonCode             string `json:"reasonCode"`
	SelectedBinding        bool   `json:"selectedBinding"`
	ProtocolSocketState    string `json:"protocolSocketState"`
}

func DecodeDiagnosticsExportPayload(data []byte) error { return networkObject(data, nil) }
func ValidDiagnosticMetadata(text string) bool {
	if len(text) == 0 || len(text) > 128 {
		return false
	}
	for _, c := range []byte(text) {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}
func diagnosticEnum(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
func (value *DiagnosticExportNetwork) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"available"}, "interfaceCount", "ipv4AssignmentCount", "upInterfaceCount", "automaticCandidateCount", "explicitBindableCount"); err != nil {
		return err
	}
	type plain DiagnosticExportNetwork
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.Available {
		if wire.InterfaceCount == nil || wire.IPv4AssignmentCount == nil || wire.UpInterfaceCount == nil || wire.AutomaticCandidateCount == nil || wire.ExplicitBindableCount == nil {
			return errors.New("missing diagnostic network counts")
		}
		if *wire.UpInterfaceCount > *wire.InterfaceCount || *wire.AutomaticCandidateCount > *wire.IPv4AssignmentCount || *wire.ExplicitBindableCount > *wire.IPv4AssignmentCount {
			return errors.New("invalid diagnostic network counts")
		}
	} else if wire.InterfaceCount != nil || wire.IPv4AssignmentCount != nil || wire.UpInterfaceCount != nil || wire.AutomaticCandidateCount != nil || wire.ExplicitBindableCount != nil {
		return errors.New("unexpected diagnostic network counts")
	}
	*value = DiagnosticExportNetwork(wire)
	return nil
}
func (value *DiagnosticExportCatalog) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"storageProtection", "totalConfigurations", "autoLoginConfigurations", "autoReconnectConfigurations", "automaticBindingConfigurations", "explicitBindingConfigurations", "availableConfigurations", "profileUnavailableConfigurations", "protocolUnavailableConfigurations", "overrideInvalidConfigurations"}); err != nil {
		return err
	}
	type plain DiagnosticExportCatalog
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if !diagnosticEnum(wire.StorageProtection, "protected", "unprotected") || wire.AutoLoginConfigurations > 1 || wire.AutoLoginConfigurations > wire.TotalConfigurations || wire.AutoReconnectConfigurations > wire.TotalConfigurations || uint64(wire.AutomaticBindingConfigurations)+uint64(wire.ExplicitBindingConfigurations) != uint64(wire.TotalConfigurations) {
		return errors.New("invalid diagnostic catalog counts")
	}
	if uint64(wire.AvailableConfigurations)+uint64(wire.ProfileUnavailableConfigurations)+uint64(wire.ProtocolUnavailableConfigurations)+uint64(wire.OverrideInvalidConfigurations) != uint64(wire.TotalConfigurations) {
		return errors.New("invalid diagnostic availability counts")
	}
	*value = DiagnosticExportCatalog(wire)
	return nil
}
func (value *DiagnosticExportSessions) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"totalCount", "truncated", "items"}); err != nil {
		return err
	}
	type plain DiagnosticExportSessions
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	count := wire.TotalCount
	if count > MaximumDiagnosticSessionItems {
		count = MaximumDiagnosticSessionItems
	}
	if wire.Items == nil || len(wire.Items) != int(count) || wire.Truncated != (wire.TotalCount > MaximumDiagnosticSessionItems) {
		return errors.New("invalid diagnostic session bounds")
	}
	*value = DiagnosticExportSessions(wire)
	return nil
}
func (value *DiagnosticExportSession) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"state", "intent", "reasonCode", "selectedBinding", "protocolSocketState", "failureCategory", "recoveryRecommendation", "cleanupRequired"}); err != nil {
		return err
	}
	type plain DiagnosticExportSession
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if !diagnosticEnum(wire.State, "suspended", "waiting_for_network", "authenticating", "authenticated", "waiting_before_retry", "blocked_by_error", "stopping", "other") || !diagnosticEnum(wire.Intent, "maintain_authentication", "suspend_authentication", "other") || !diagnosticEnum(wire.ReasonCode, "network_binding_unavailable", "network_unavailable", "runtime_definition_unavailable", "protocol_run_creation_failed", "protocol_run_failed", "protocol_contract_violated", "automatic_reconnect_disabled", "none", "other") || !diagnosticEnum(wire.ProtocolSocketState, "not_observed", "open", "closed", "close_failed", "close_unconfirmed", "other") {
		return errors.New("invalid diagnostic session category")
	}
	if !diagnosticEnum(wire.FailureCategory, "none", "network_timeout", "network_io_failure", "server_busy", "authentication_rejected", "binding_rejected", "protocol_incompatible", "protocol_response_invalid", "protocol_contract_violated", "logout_cleanup_failed", "run_creation_failed", "runtime_definition_unavailable", "other") || !diagnosticEnum(wire.RecoveryRecommendation, "none", "retry_after_standard_delay", "retry_after_extended_delay", "block_until_explicit_restart_or_relevant_input_change", "other") {
		return errors.New("invalid diagnostic recovery category")
	}
	*value = DiagnosticExportSession(wire)
	return nil
}
func (value *DiagnosticsExportResult) UnmarshalJSON(data []byte) error {
	if len(data) > MaximumDiagnosticExportBytes {
		return errors.New("diagnostic export exceeds size limit")
	}
	if err := networkObject(data, []string{"schemaVersion", "generatedAt", "productVersion", "buildId", "operatingSystem", "architecture", "network", "catalog", "sessions"}); err != nil {
		return err
	}
	type plain DiagnosticsExportResult
	var wire plain
	if err := decodeStrict(data, &wire); err != nil {
		return err
	}
	if wire.SchemaVersion != 1 || !networkTime(wire.GeneratedAt) || !ValidDiagnosticMetadata(wire.ProductVersion) || !ValidDiagnosticMetadata(wire.BuildID) || !diagnosticEnum(wire.OperatingSystem, "windows", "linux", "darwin", "other") || !diagnosticEnum(wire.Architecture, "amd64", "arm64", "386", "other") {
		return errors.New("invalid diagnostic export metadata")
	}
	*value = DiagnosticsExportResult(wire)
	return nil
}
func DecodeDiagnosticsExportResult(data []byte) (DiagnosticsExportResult, error) {
	if len(data) > MaximumDiagnosticExportBytes {
		return DiagnosticsExportResult{}, errors.New("diagnostic export exceeds size limit")
	}
	var value DiagnosticsExportResult
	err := decodeStrict(data, &value)
	return value, err
}
func MarshalDiagnosticsExportResult(value DiagnosticsExportResult) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeDiagnosticsExportResult(data); err != nil {
		return nil, err
	}
	return data, nil
}
