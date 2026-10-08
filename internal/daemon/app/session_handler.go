package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/daemon/authentication/supervisor"
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/ipc/contract"
)

// sessionApplication is the narrow application boundary used by SessionHandler.
// It is implemented by Application and intentionally exposes only the one-shot
// start, stop and get operations, so the IPC handler never depends on
// Configuration, Credentials, Supervisor or Session internals.
type sessionApplication interface {
	StartOneShotAuthentication(ctx context.Context, input OneShotAuthenticationInput) (SessionStartResult, error)
	StartConfigurationAuthentication(ctx context.Context, id config.ConfigurationID) (SessionStartResult, error)
	StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error)
	EnsureSessionRunning(ctx context.Context, sessionID session.AuthenticationSessionID) (SessionStartResult, error)
	RestartSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error)
	RemoveSession(ctx context.Context, sessionID session.AuthenticationSessionID) error
	GetSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error)
	ListSessionView(ctx context.Context) (SessionListView, error)
}

var _ sessionApplication = (*Application)(nil)

// SessionHandler returns an IPC handler for one-shot Session operations. It
// strictly decodes the typed payload selected by the method, converts it into
// existing app/session domain types, calls only the narrow application boundary,
// and converts the returned Snapshot into the contract result. Methods other
// than the three Session methods return unknown_method without calling the
// application.
func SessionHandler(application sessionApplication) func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		switch method {
		case contract.MethodSessionStartOneShot:
			return handleSessionStartOneShot(ctx, application, payload)
		case contract.MethodSessionStartConfiguration:
			return handleSessionStartConfiguration(ctx, application, payload)
		case contract.MethodSessionStop:
			return handleSessionStop(ctx, application, payload)
		case contract.MethodSessionEnsureRunning:
			return handleSessionEnsureRunning(ctx, application, payload)
		case contract.MethodSessionRestart:
			return handleSessionRestart(ctx, application, payload)
		case contract.MethodSessionRemove:
			return handleSessionRemove(ctx, application, payload)
		case contract.MethodSessionGet:
			return handleSessionGet(ctx, application, payload)
		case contract.MethodSessionList:
			return handleSessionList(ctx, application, payload)
		default:
			return nil, &contract.Error{
				Code:    contract.ErrorCodeUnknownMethod,
				Message: "unsupported session method",
			}
		}
	}
}

func handleSessionStartConfiguration(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeConfigurationIDPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	result, err := application.StartConfigurationAuthentication(ctx, config.ConfigurationID(request.ConfigurationID))
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionStartResult(result)
}

func handleSessionEnsureRunning(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeSessionEnsureRunningPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	result, err := application.EnsureSessionRunning(ctx, session.AuthenticationSessionID(request.SessionID))
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionStartResult(result)
}

func handleSessionRestart(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeSessionRestartPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	snapshot, err := application.RestartSession(ctx, session.AuthenticationSessionID(request.SessionID))
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionResult(snapshot)
}

func handleSessionRemove(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeSessionRemovePayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	if err := application.RemoveSession(ctx, session.AuthenticationSessionID(request.SessionID)); err != nil {
		return nil, sessionError(err)
	}
	result, err := contract.MarshalSessionRemoveResult(contract.SessionRemoveResult{SessionID: request.SessionID, Status: "removed"})
	if err != nil {
		return nil, sessionError(err)
	}
	return result, nil
}

func handleSessionList(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	if err := contract.DecodeEmptyPayload(payload); err != nil {
		return nil, invalidArgumentError()
	}
	view, err := application.ListSessionView(ctx)
	if err != nil {
		return nil, sessionError(err)
	}
	results := make([]contract.SessionResult, 0, len(view.Sessions))
	for _, snapshot := range view.Sessions {
		results = append(results, toSessionResult(snapshot))
	}
	cleanup := make([]string, 0, len(view.CleanupRequiredSessionIDs))
	for _, id := range view.CleanupRequiredSessionIDs {
		cleanup = append(cleanup, string(id))
	}
	result, err := contract.MarshalSessionListResult(contract.SessionListResult{Sessions: results, CleanupRequiredSessionIDs: cleanup})
	if err != nil {
		return nil, &contract.Error{
			Code:    contract.ErrorCodeSessionOperationFailed,
			Message: "failed to encode session list",
		}
	}
	return result, nil
}

func handleSessionStartOneShot(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeSessionStartOneShotPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}

	policy, _ := request.NetworkBindingPolicy.Domain()
	input := OneShotAuthenticationInput{
		DisplayName:          request.DisplayName,
		InstitutionProfileID: config.InstitutionProfileID(request.InstitutionProfileID),
		AuthenticationCredential: credential.AuthenticationCredential{
			Username: request.Username,
			Password: request.Password,
		},
		NetworkBindingPolicy:    policy,
		ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(request.ProtocolContextOverride),
	}

	result, err := application.StartOneShotAuthentication(ctx, input)
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionStartResult(result)
}

func handleSessionStop(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeSessionStopPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	snapshot, err := application.StopSession(ctx, session.AuthenticationSessionID(request.SessionID))
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionResult(snapshot)
}

func handleSessionGet(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	request, err := contract.DecodeSessionGetPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	snapshot, err := application.GetSession(ctx, session.AuthenticationSessionID(request.SessionID))
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionResult(snapshot)
}

func encodeSessionResult(snapshot session.Snapshot) (json.RawMessage, *contract.Error) {
	result, err := contract.MarshalSessionResult(toSessionResult(snapshot))
	if err != nil {
		return nil, &contract.Error{
			Code:    contract.ErrorCodeSessionOperationFailed,
			Message: "failed to encode session result",
		}
	}
	return result, nil
}

func encodeSessionStartResult(result SessionStartResult) (json.RawMessage, *contract.Error) {
	data, err := contract.MarshalSessionStartResult(contract.SessionStartResult{
		Outcome: result.Outcome,
		Session: toSessionResult(result.Snapshot),
	})
	if err != nil {
		return nil, &contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "failed to encode session start result"}
	}
	return data, nil
}

func invalidArgumentError() *contract.Error {
	return &contract.Error{
		Code:    contract.ErrorCodeInvalidArgument,
		Message: "malformed session payload",
	}
}

// sessionError maps an application or Supervisor failure to a stable public
// error. Known ResolutionFailure codes map to their corresponding stable code;
// every other failure maps to session_operation_failed. The message is generic
// and never includes the username, password, opaque override or wrapped cause.
func sessionError(err error) *contract.Error {
	switch {
	case errors.Is(err, supervisor.ErrSessionNotFound):
		return &contract.Error{Code: contract.ErrorCodeSessionNotFound, Message: "session not found"}
	case errors.Is(err, supervisor.ErrActiveSessionConflict):
		return &contract.Error{Code: contract.ErrorCodeSessionActiveConflict, Message: "another active session exists"}
	case errors.Is(err, supervisor.ErrSessionStateConflict):
		return &contract.Error{Code: contract.ErrorCodeSessionStateConflict, Message: "session state does not allow operation"}
	}
	var resolutionFailure *ResolutionFailure
	if errors.As(err, &resolutionFailure) {
		switch resolutionFailure.Code() {
		case ProfileNotFound:
			return &contract.Error{Code: contract.ErrorCodeProfileNotFound, Message: "institution profile not found"}
		case ProtocolNotFound:
			return &contract.Error{Code: contract.ErrorCodeProtocolNotFound, Message: "authentication protocol not found"}
		case ConfigurationNotFound:
			return &contract.Error{Code: contract.ErrorCodeConfigurationNotFound, Message: "configuration not found"}
		case InvalidConfiguration, InvalidEnvironment:
			return &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "invalid session request"}
		}
	}
	return &contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "session operation failed"}
}

// toSessionResult maps every public session.Snapshot field into the contract
// result. It drops username, password, raw protocol
// configuration, the raw protocol context override and diagnostic causes, which
// never appear in a public Snapshot.
func toSessionResult(snapshot session.Snapshot) contract.SessionResult {
	result := contract.SessionResult{
		ProtocolSocket:           *networkProtocolSocket(snapshot.ProtocolSocket),
		AuthenticationSessionID:  string(snapshot.AuthenticationSessionID),
		ConfigurationID:          string(snapshot.ConfigurationID),
		DisplayName:              snapshot.DisplayName,
		InstitutionProfileID:     string(snapshot.InstitutionProfileID),
		InstitutionDisplayName:   snapshot.InstitutionDisplayName,
		AuthenticationProtocolID: string(snapshot.AuthenticationProtocolID),
		AccountName:              snapshot.AccountName,
		Intent:                   string(snapshot.Intent),
		State:                    string(snapshot.State),
		Revision:                 snapshot.Revision,
		UpdatedAt:                snapshot.UpdatedAt.Format(time.RFC3339Nano),
	}
	if snapshot.StateReason != nil {
		result.StateReason = &contract.SessionStateReason{
			Code:        snapshot.StateReason.Code,
			Description: snapshot.StateReason.Description,
		}
	}
	if snapshot.SelectedNetworkBinding != nil {
		result.SelectedNetworkBinding = &contract.SessionNetworkBinding{
			InterfaceID:      string(snapshot.SelectedNetworkBinding.InterfaceID),
			DisplayName:      snapshot.SelectedNetworkBinding.DisplayName,
			LocalIPv4Address: snapshot.SelectedNetworkBinding.LocalIPv4Address.String(),
		}
	}
	if snapshot.AuthenticationEstablishedAt != nil {
		established := snapshot.AuthenticationEstablishedAt.Format(time.RFC3339Nano)
		result.AuthenticationEstablishedAt = &established
	}
	if snapshot.NextRetryAt != nil {
		retry := snapshot.NextRetryAt.Format(time.RFC3339Nano)
		result.NextRetryAt = &retry
	}
	if snapshot.LastAuthenticationFailure != nil {
		result.LastAuthenticationFailure = &contract.SessionAuthenticationFailure{
			Code:                   string(snapshot.LastAuthenticationFailure.Code),
			Description:            snapshot.LastAuthenticationFailure.Description,
			HandlingRecommendation: string(snapshot.LastAuthenticationFailure.HandlingRecommendation),
		}
	}
	return result
}
