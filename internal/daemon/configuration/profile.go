package configuration

import "sidravia/internal/daemon/authentication/protocol"

type InstitutionProfileID string

type InstitutionProfile struct {
	InstitutionProfileID             InstitutionProfileID
	DisplayName                      string
	AuthenticationProtocolID         protocol.AuthenticationProtocolID
	InstitutionProtocolConfiguration protocol.InstitutionProtocolConfiguration
}

type InstitutionProfileSummary struct {
	InstitutionProfileID     InstitutionProfileID
	DisplayName              string
	AuthenticationProtocolID protocol.AuthenticationProtocolID
}

func (profile InstitutionProfile) Clone() InstitutionProfile {
	profile.InstitutionProtocolConfiguration = append(
		protocol.InstitutionProtocolConfiguration(nil),
		profile.InstitutionProtocolConfiguration...,
	)
	return profile
}
