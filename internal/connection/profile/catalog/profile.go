package catalog

import "sidravia/internal/connection/profile"

func validateInstitutionProfileID(id profile.ID) error {
	if !id.Valid() {
		return profileCatalogInvalidArgument(nil)
	}
	return nil
}
