package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

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

func DecodeProfileListResult(data []byte) (ProfileListResult, error) {
	var result ProfileListResult
	if err := decodeProfileList(data, &result); err != nil {
		return ProfileListResult{}, err
	}
	if result.Profiles == nil {
		return ProfileListResult{}, fmt.Errorf("Profile 列表缺少 profiles 字段")
	}
	for _, profile := range result.Profiles {
		if profile.InstitutionProfileID == "" ||
			profile.DisplayName == "" ||
			profile.AuthenticationProtocolID == "" {
			return ProfileListResult{}, fmt.Errorf("Profile 列表包含不完整的 Profile")
		}
	}
	return result, nil
}

func decodeProfileList(data []byte, destination *ProfileListResult) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("列表结果之后存在多余数据")
	}
	return nil
}
