package config

import (
	"sidravia/internal/connection/binding"
	"sidravia/internal/connection/profile"
)

// Patch describes changes to public connection settings. Nil fields retain the
// saved value; a non-nil pointer to an empty override removes it.
type Patch struct {
	DisplayName             *string
	InstitutionProfileID    *profile.ID
	Username                *string
	NetworkBindingPolicy    *binding.Policy
	ProtocolContextOverride *profile.ProtocolOverride
	AutoLogin               *bool
	AutoReconnect           *bool
}

func (patch Patch) Empty() bool {
	return patch.DisplayName == nil && patch.InstitutionProfileID == nil && patch.Username == nil &&
		patch.NetworkBindingPolicy == nil && patch.ProtocolContextOverride == nil &&
		patch.AutoLogin == nil && patch.AutoReconnect == nil
}

// Apply returns an independent value for validation or persistence.
func (patch Patch) Apply(current Configuration) Configuration {
	next := current.Clone()
	if patch.DisplayName != nil {
		next.DisplayName = *patch.DisplayName
	}
	if patch.InstitutionProfileID != nil {
		next.InstitutionProfileID = *patch.InstitutionProfileID
	}
	if patch.Username != nil {
		next.Username = *patch.Username
	}
	if patch.NetworkBindingPolicy != nil {
		next.NetworkBindingPolicy = *patch.NetworkBindingPolicy
	}
	if patch.ProtocolContextOverride != nil {
		next.ProtocolContextOverride = append(profile.ProtocolOverride(nil), (*patch.ProtocolContextOverride)...)
	}
	if patch.AutoLogin != nil {
		next.AutoLogin = *patch.AutoLogin
	}
	if patch.AutoReconnect != nil {
		next.AutoReconnect = *patch.AutoReconnect
	}
	return next
}
