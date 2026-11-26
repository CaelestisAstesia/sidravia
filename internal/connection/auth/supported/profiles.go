// Package supported owns the set of protocol descriptions shipped by Sidravia.
package supported

import (
	"fmt"

	"sidravia/internal/connection/auth/d520"
	"sidravia/internal/connection/profile"
)

var descriptions = map[profile.ProtocolID]func(profile.ProtocolConfiguration) error{
	d520.ProtocolID: d520.ValidateInstitution,
}

func ValidateInstitution(id profile.ProtocolID, raw profile.ProtocolConfiguration) error {
	validate, ok := descriptions[id]
	if !ok {
		return fmt.Errorf("authentication protocol %q is not registered", id)
	}
	return validate(raw)
}

// CheckDrivers requires one runtime driver for each shipped description.
func CheckDrivers(ids ...profile.ProtocolID) error {
	seen := make(map[profile.ProtocolID]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := descriptions[id]; !exists {
			return fmt.Errorf("authentication protocol %q has no supported description", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate authentication protocol ID %q", id)
		}
		seen[id] = struct{}{}
	}
	for id := range descriptions {
		if _, exists := seen[id]; !exists {
			return fmt.Errorf("authentication protocol %q has no runtime driver", id)
		}
	}
	return nil
}
