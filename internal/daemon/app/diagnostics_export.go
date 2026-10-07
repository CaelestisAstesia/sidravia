package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"time"

	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/environment"
	"sidravia/internal/ipc/contract"
)

// ExportDiagnostics collects sequential per-owner observations. The operation
// lock serializes application intents, not the independently advancing actors.
// Only the curated contract is serialized; source DTOs never enter the artifact.
func (application *Application) ExportDiagnostics(ctx context.Context, productVersion, buildID string) (contract.DiagnosticsExportResult, error) {
	if ctx == nil {
		return contract.DiagnosticsExportResult{}, errors.New("diagnostic export context is required")
	}
	if ctx.Err() != nil {
		return contract.DiagnosticsExportResult{}, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	application.opMu.Lock()
	defer application.opMu.Unlock()
	values, err := application.catalog.List(ctx)
	if err != nil {
		return contract.DiagnosticsExportResult{}, fmt.Errorf("diagnostic export catalog: %w", errors.Join(err, context.Cause(ctx)))
	}
	catalog, err := diagnosticCatalog(values, string(application.catalog.StorageProtection()))
	if err != nil {
		return contract.DiagnosticsExportResult{}, err
	}
	snapshots, err := application.sup.List(ctx)
	if err != nil {
		return contract.DiagnosticsExportResult{}, fmt.Errorf("diagnostic export sessions: %w", errors.Join(err, context.Cause(ctx)))
	}
	total, err := diagnosticCount(uint64(len(snapshots)))
	if err != nil {
		return contract.DiagnosticsExportResult{}, err
	}
	sessions := contract.DiagnosticExportSessions{TotalCount: total, Truncated: len(snapshots) > contract.MaximumDiagnosticSessionItems, Items: []contract.DiagnosticExportSession{}}
	count := len(snapshots)
	if count > contract.MaximumDiagnosticSessionItems {
		count = contract.MaximumDiagnosticSessionItems
	}
	for _, snapshot := range snapshots[:count] {
		paired, err := application.sup.GetNetworkDiagnostics(ctx, snapshot.AuthenticationSessionID)
		if err != nil {
			return contract.DiagnosticsExportResult{}, fmt.Errorf("diagnostic export actor: %w", errors.Join(err, context.Cause(ctx)))
		}
		sessions.Items = append(sessions.Items, diagnosticSession(paired))
	}
	snapshot, available, err := application.sup.LatestSystemNetworkSnapshot(ctx)
	if err != nil {
		return contract.DiagnosticsExportResult{}, fmt.Errorf("diagnostic export network: %w", errors.Join(err, context.Cause(ctx)))
	}
	network, err := diagnosticNetwork(snapshot, available)
	if err != nil {
		return contract.DiagnosticsExportResult{}, err
	}
	if ctx.Err() != nil {
		return contract.DiagnosticsExportResult{}, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	result := contract.DiagnosticsExportResult{SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), ProductVersion: diagnosticMetadata(productVersion), BuildID: diagnosticMetadata(buildID), OperatingSystem: diagnosticCategory(runtime.GOOS, "windows", "linux", "darwin"), Architecture: diagnosticCategory(runtime.GOARCH, "amd64", "arm64", "386"), Network: network, Catalog: catalog, Sessions: sessions}
	return result, nil
}
func diagnosticMetadata(text string) string {
	if contract.ValidDiagnosticMetadata(text) {
		return text
	}
	return "unknown"
}
func diagnosticCategory(value string, allowed ...string) string {
	for _, item := range allowed {
		if value == item {
			return value
		}
	}
	return "other"
}
func diagnosticCount(count uint64) (uint32, error) {
	if count > uint64(^uint32(0)) {
		return 0, errors.New("diagnostic count exceeds limit")
	}
	return uint32(count), nil
}
func diagnosticCatalog(values []config.Configuration, protection string) (contract.DiagnosticExportCatalog, error) {
	result := contract.DiagnosticExportCatalog{StorageProtection: protection}
	if protection != "protected" && protection != "unprotected" {
		return result, errors.New("unsupported diagnostic storage protection")
	}
	total, err := diagnosticCount(uint64(len(values)))
	if err != nil {
		return result, err
	}
	result.TotalConfigurations = total
	for _, value := range values {
		if value.AutoLogin {
			result.AutoLoginConfigurations++
		}
		if value.AutoReconnect {
			result.AutoReconnectConfigurations++
		}
		switch value.NetworkBindingPolicy.Mode {
		case config.AutomaticallySelectLatestAvailable:
			result.AutomaticBindingConfigurations++
		case config.ExplicitInterfaceAndLocalIPv4:
			result.ExplicitBindingConfigurations++
		default:
			return result, errors.New("unsupported diagnostic binding mode")
		}
	}
	if result.AutoLoginConfigurations > 1 {
		return result, errors.New("invalid diagnostic automatic login count")
	}
	return result, nil
}
func diagnosticNetwork(snapshot environment.Snapshot, available bool) (contract.DiagnosticExportNetwork, error) {
	result := contract.DiagnosticExportNetwork{Available: available}
	if !available {
		return result, nil
	}
	interfaces := snapshot.Interfaces()
	ifaceCount, err := diagnosticCount(uint64(len(interfaces)))
	if err != nil {
		return result, err
	}
	var addresses uint64
	for _, iface := range interfaces {
		addresses += uint64(len(iface.IPv4AddressAssignments()))
		if addresses > uint64(^uint32(0)) {
			return result, errors.New("diagnostic count exceeds limit")
		}
	}
	addressCount, err := diagnosticCount(addresses)
	if err != nil {
		return result, err
	}
	result.InterfaceCount, result.IPv4AssignmentCount = &ifaceCount, &addressCount
	return result, nil
}
func diagnosticSession(paired session.NetworkDiagnosticsSnapshot) contract.DiagnosticExportSession {
	snapshot := paired.Snapshot
	reason := "none"
	if snapshot.StateReason != nil {
		reason = diagnosticCategory(snapshot.StateReason.Code, session.StateReasonCodeNetworkBindingUnavailable, session.StateReasonCodeNetworkUnavailable, session.StateReasonCodeRuntimeDefinitionUnavailable, session.StateReasonCodeProtocolRunCreationFailed, session.StateReasonCodeProtocolRunFailed, session.StateReasonCodeProtocolContractViolated, session.StateReasonCodeAutomaticReconnectDisabled)
	}
	return contract.DiagnosticExportSession{State: diagnosticCategory(string(snapshot.State), string(session.Suspended), string(session.WaitingForNetwork), string(session.Authenticating), string(session.Authenticated), string(session.WaitingBeforeRetry), string(session.BlockedByError), string(session.Stopping)), Intent: diagnosticCategory(string(snapshot.Intent), string(session.MaintainAuthentication), string(session.SuspendAuthentication)), ReasonCode: reason, SelectedBinding: snapshot.SelectedNetworkBinding != nil, ProtocolSocketState: diagnosticCategory(string(paired.ProtocolSocket.State), string(session.ProtocolSocketNotObserved), string(session.ProtocolSocketOpen), string(session.ProtocolSocketClosed), string(session.ProtocolSocketCloseFailed), string(session.ProtocolSocketCloseUnconfirmed))}
}
func DiagnosticsExportHandler(application *Application, productVersion, buildID string) func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodDiagnosticsExport {
			return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported diagnostic method"}
		}
		if err := contract.DecodeDiagnosticsExportPayload(payload); err != nil {
			return nil, &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "malformed diagnostic payload"}
		}
		value, err := application.ExportDiagnostics(ctx, productVersion, buildID)
		if err != nil {
			return nil, &contract.Error{Code: contract.ErrorCodeInternalError, Message: "diagnostic export failed"}
		}
		raw, err := contract.MarshalDiagnosticsExportResult(value)
		if err != nil {
			return nil, &contract.Error{Code: contract.ErrorCodeInternalError, Message: "diagnostic export failed"}
		}
		return raw, nil
	}
}
