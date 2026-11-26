// Package d520 describes the institution parameters supported by the D520 protocol.
package d520

import (
	"sidravia/internal/connection/auth/d520/internal/settings"
	"sidravia/internal/connection/profile"
)

// ProtocolID identifies both the regular and explicitly selected experimental behavior.
const ProtocolID profile.ProtocolID = "drcom-5.2.0-d"

// ValidateInstitution applies the exact static rules used by the runtime.
func ValidateInstitution(raw profile.ProtocolConfiguration) error {
	_, err := settings.DecodeInstitution(raw)
	return err
}
