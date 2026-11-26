package catalog

import (
	"context"
	"sort"
	"sync"

	profilevalue "sidravia/internal/connection/profile"
	"sidravia/internal/document"
)

type ProfileCatalog struct {
	mu       sync.RWMutex
	profiles map[profilevalue.ID]profilevalue.Profile
}

func NewProfileCatalog(profiles []profilevalue.Profile) (*ProfileCatalog, error) {
	catalog := make(map[profilevalue.ID]profilevalue.Profile, len(profiles))
	for _, profile := range profiles {
		if err := validateProfile(profile); err != nil {
			return nil, err
		}
		if _, exists := catalog[profile.InstitutionProfileID]; exists {
			return nil, document.NewFailure(document.FailureConflict, nil)
		}
		catalog[profile.InstitutionProfileID] = profile.Clone()
	}
	return &ProfileCatalog{profiles: catalog}, nil
}

func (catalog *ProfileCatalog) Get(ctx context.Context, id profilevalue.ID) (profilevalue.Profile, error) {
	if err := validateInstitutionProfileID(id); err != nil {
		return profilevalue.Profile{}, err
	}
	if err := validateProfileCatalogContext(ctx); err != nil {
		return profilevalue.Profile{}, err
	}
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	profile, exists := catalog.profiles[id]
	if !exists {
		return profilevalue.Profile{}, document.NewFailure(document.FailureNotFound, nil)
	}
	return profile.Clone(), nil
}

func (catalog *ProfileCatalog) ListSummaries(ctx context.Context) ([]profilevalue.Summary, error) {
	if err := validateProfileCatalogContext(ctx); err != nil {
		return nil, err
	}
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	identifiers := make([]profilevalue.ID, 0, len(catalog.profiles))
	for identifier := range catalog.profiles {
		identifiers = append(identifiers, identifier)
	}
	sort.Slice(identifiers, func(left, right int) bool { return identifiers[left] < identifiers[right] })
	summaries := make([]profilevalue.Summary, 0, len(identifiers))
	for _, identifier := range identifiers {
		profile := catalog.profiles[identifier]
		summaries = append(summaries, profilevalue.Summary{
			InstitutionProfileID:     profile.InstitutionProfileID,
			DisplayName:              profile.DisplayName,
			AuthenticationProtocolID: profile.AuthenticationProtocolID,
			Announcement:             profile.Announcement.Clone(),
		})
	}
	return summaries, nil
}

func validateProfile(profile profilevalue.Profile) error {
	if err := validateInstitutionProfileID(profile.InstitutionProfileID); err != nil {
		return err
	}
	if err := profile.Announcement.Validate(); err != nil {
		return profileCatalogInvalidArgument(err)
	}
	switch {
	case profile.DisplayName == "":
		return profileCatalogInvalidArgument(nil)
	case profile.AuthenticationProtocolID == "":
		return profileCatalogInvalidArgument(nil)
	default:
		return nil
	}
}

func validateProfileCatalogContext(ctx context.Context) error {
	if ctx == nil {
		return profileCatalogInvalidArgument(nil)
	}
	if err := ctx.Err(); err != nil {
		return profileCatalogInvalidArgument(err)
	}
	return nil
}

func profileCatalogInvalidArgument(cause error) error {
	return document.NewFailure(document.FailureInvalidArgument, cause)
}
