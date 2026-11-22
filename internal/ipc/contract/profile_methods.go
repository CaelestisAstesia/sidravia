package contract

import "encoding/json"

const (
	MethodProfileList = "profile.list"
)

type ProfileAnnouncementResult struct {
	URL          string `json:"url"`
	ClientWidth  int    `json:"clientWidth"`
	ClientHeight int    `json:"clientHeight"`
}

type ProfileSummaryResult struct {
	OverrideSchemaVersion    int                        `json:"overrideSchemaVersion,omitempty"`
	Announcement             *ProfileAnnouncementResult `json:"announcement,omitempty"`
	InstitutionProfileID     string                     `json:"institutionProfileId"`
	DisplayName              string                     `json:"displayName"`
	AuthenticationProtocolID string                     `json:"authenticationProtocolId"`
}

type ProfileListResult struct {
	Profiles []ProfileSummaryResult `json:"profiles"`
}

func MarshalProfileListResult(result ProfileListResult) (json.RawMessage, error) {
	if result.Profiles == nil {
		result.Profiles = []ProfileSummaryResult{}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
