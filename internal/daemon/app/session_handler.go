package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
	credential "sidravia/internal/daemon/credentials"
	"sidravia/internal/ipc/contract"
)

// sessionApplication is the narrow application boundary used by SessionHandler.
// It is implemented by Application and intentionally exposes only the one-shot
// start, stop and get operations, so the IPC handler never depends on
// Configuration, Credentials, Supervisor or Session internals.
type sessionApplication interface {
	StartOneShotAuthentication(ctx context.Context, input OneShotAuthenticationInput) (session.AuthenticationSessionID, session.Snapshot, error)
	StopSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error)
	GetSession(ctx context.Context, sessionID session.AuthenticationSessionID) (session.Snapshot, error)
	ListSessions(ctx context.Context) ([]session.Snapshot, error)
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
		case contract.MethodSessionStop:
			return handleSessionStop(ctx, application, payload)
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

func handleSessionList(ctx context.Context, application sessionApplication, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	if err := contract.DecodeEmptyPayload(payload); err != nil {
		return nil, invalidArgumentError()
	}
	snapshots, err := application.ListSessions(ctx)
	if err != nil {
		return nil, sessionError(err)
	}
	results := make([]contract.SessionResult, 0, len(snapshots))
	for _, snapshot := range snapshots {
		results = append(results, toSessionResult(snapshot))
	}
	result, err := contract.MarshalSessionListResult(contract.SessionListResult{Sessions: results})
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

	input := OneShotAuthenticationInput{
		DisplayName:          request.DisplayName,
		InstitutionProfileID: config.InstitutionProfileID(request.InstitutionProfileID),
		AuthenticationCredential: credential.AuthenticationCredential{
			Username: request.Username,
			Password: request.Password,
		},
		NetworkBindingPolicy: session.NetworkBindingPolicy{
			Mode: session.NetworkBindingPolicyMode(request.NetworkBindingPolicyMode),
		},
		ProtocolContextOverride: protocol.AuthenticationProtocolContextOverride(request.ProtocolContextOverride),
	}

	_, snapshot, err := application.StartOneShotAuthentication(ctx, input)
	if err != nil {
		return nil, sessionError(err)
	}
	return encodeSessionResult(snapshot)
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
	var resolutionFailure *ResolutionFailure
	if errors.As(err, &resolutionFailure) {
		switch resolutionFailure.Code() {
		case ProfileNotFound:
			return &contract.Error{Code: contract.ErrorCodeProfileNotFound, Message: "institution profile not found"}
		case ProtocolNotFound:
			return &contract.Error{Code: contract.ErrorCodeProtocolNotFound, Message: "authentication protocol not found"}
		case ConfigurationNotFound, CredentialNotFound, InvalidConfiguration, InvalidEnvironment:
			return &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "invalid session request"}
		}
	}
	return &contract.Error{Code: contract.ErrorCodeSessionOperationFailed, Message: "session operation failed"}
}

// toSessionResult maps every public session.Snapshot field into the contract
// result. It drops CredentialID, username, password, raw protocol
// configuration, the raw protocol context override and diagnostic causes, which
// never appear in a public Snapshot.
func toSessionResult(snapshot session.Snapshot) contract.SessionResult {
	result := contract.SessionResult{
		AuthenticationSessionID:  string(snapshot.AuthenticationSessionID),
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
