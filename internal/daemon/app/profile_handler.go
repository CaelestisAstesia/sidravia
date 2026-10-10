package app

import (
	"context"
	"encoding/json"

	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/ipc/contract"
)

type profileApplication interface {
	ListInstitutionProfiles(context.Context) ([]config.InstitutionProfileSummary, error)
}

var _ profileApplication = (*Application)(nil)

func ProfileHandler(application profileApplication) func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		if method != contract.MethodProfileList {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeUnknownMethod,
				Message: "unsupported profile method",
			}
		}
		if err := contract.DecodeEmptyPayload(payload); err != nil {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeInvalidArgument,
				Message: "malformed profile payload",
			}
		}
		summaries, err := application.ListInstitutionProfiles(ctx)
		if err != nil {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeProfileOperationFailed,
				Message: "profile operation failed",
			}
		}
		results := make([]contract.ProfileSummaryResult, 0, len(summaries))
		for _, summary := range summaries {
			var announcement *contract.ProfileAnnouncementResult
			if summary.Announcement != nil {
				announcement = &contract.ProfileAnnouncementResult{URL: summary.Announcement.URL, ClientWidth: summary.Announcement.ClientWidth, ClientHeight: summary.Announcement.ClientHeight}
			}
			results = append(results, contract.ProfileSummaryResult{
				InstitutionProfileID:     string(summary.InstitutionProfileID),
				Announcement:             announcement,
				DisplayName:              summary.DisplayName,
				AuthenticationProtocolID: string(summary.AuthenticationProtocolID),
			})
		}
		result, err := contract.MarshalProfileListResult(contract.ProfileListResult{Profiles: results})
		if err != nil {
			return nil, &contract.Error{
				Code:    contract.ErrorCodeProfileOperationFailed,
				Message: "failed to encode profile list",
			}
		}
		return result, nil
	}
}
