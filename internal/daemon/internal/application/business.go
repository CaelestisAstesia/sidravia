package application

import (
	"context"
	"encoding/json"
	"time"

	"sidravia/internal/daemon/internal/session"
	"sidravia/internal/ipc/contract"
)

func (application *Application) GetSessionBusiness(ctx context.Context, id session.AuthenticationSessionID) (session.BusinessSnapshot, error) {
	return application.supervisor.GetBusiness(ctx, id)
}

type businessApplication interface {
	GetSessionBusiness(context.Context, session.AuthenticationSessionID) (session.BusinessSnapshot, error)
}

func dispatchBusiness(application businessApplication, ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	if method != contract.MethodSessionBusinessGet {
		return nil, &contract.Error{Code: contract.ErrorCodeUnknownMethod, Message: "unsupported business method"}
	}
	request, err := contract.DecodeSessionGetPayload(payload)
	if err != nil {
		return nil, invalidArgumentError()
	}
	value, err := application.GetSessionBusiness(ctx, session.AuthenticationSessionID(request.SessionID))
	if err != nil {
		return nil, sessionError(err)
	}
	result := contract.BusinessResult{SessionID: string(value.SessionID), RunGeneration: value.RunGeneration, Revision: value.Revision, Current: value.Current, StatisticsSupported: value.StatisticsSupported, TextSupported: value.TextSupported, UpdatedAt: value.UpdatedAt.Format(time.RFC3339Nano), Messages: make([]contract.BusinessMessage, 0, len(value.Messages))}
	if s := value.Statistics; s != nil {
		result.Statistics = &contract.BusinessStatistics{ReceivedAt: s.ReceivedAt.Format(time.RFC3339Nano), Source: s.Source, Scope: s.Scope, TrafficBytes: s.TrafficBytes, BalanceCents: s.BalanceCents}
	}
	for _, m := range value.Messages {
		result.Messages = append(result.Messages, contract.BusinessMessage{ID: m.ID, ReceivedAt: m.ReceivedAt.Format(time.RFC3339Nano), Text: m.Text})
	}
	data, err := contract.MarshalBusinessResult(result)
	if err != nil {
		return nil, sessionError(err)
	}
	return data, nil
}
