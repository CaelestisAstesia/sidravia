package configuration

import (
	"context"
	"sort"
	"sync"

	"sidravia/internal/daemon/persistence"
)

type ProfileCatalog struct {
	mu       sync.RWMutex
	profiles map[InstitutionProfileID]InstitutionProfile
}

func NewProfileCatalog(profiles []InstitutionProfile) (*ProfileCatalog, error) {
	catalog := make(map[InstitutionProfileID]InstitutionProfile, len(profiles))
	for _, profile := range profiles {
		if err := validateProfile(profile); err != nil {
			return nil, err
		}
		if _, exists := catalog[profile.InstitutionProfileID]; exists {
			return nil, persistence.NewFailure(persistence.FailureConflict, nil)
		}
		catalog[profile.InstitutionProfileID] = profile.Clone()
	}
	return &ProfileCatalog{profiles: catalog}, nil
}

func (catalog *ProfileCatalog) Get(ctx context.Context, id InstitutionProfileID) (InstitutionProfile, error) {
	if err := validateInstitutionProfileID(id); err != nil {
		return InstitutionProfile{}, err
	}
	if err := validateProfileCatalogContext(ctx); err != nil {
		return InstitutionProfile{}, err
	}
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	profile, exists := catalog.profiles[id]
	if !exists {
		return InstitutionProfile{}, persistence.NewFailure(persistence.FailureNotFound, nil)
	}
	return profile.Clone(), nil
}

func (catalog *ProfileCatalog) ListSummaries(ctx context.Context) ([]InstitutionProfileSummary, error) {
	if err := validateProfileCatalogContext(ctx); err != nil {
		return nil, err
	}
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	identifiers := make([]InstitutionProfileID, 0, len(catalog.profiles))
	for identifier := range catalog.profiles {
		identifiers = append(identifiers, identifier)
	}
	sort.Slice(identifiers, func(left, right int) bool { return identifiers[left] < identifiers[right] })
	summaries := make([]InstitutionProfileSummary, 0, len(identifiers))
	for _, identifier := range identifiers {
		profile := catalog.profiles[identifier]
		summaries = append(summaries, InstitutionProfileSummary{
			InstitutionProfileID:     profile.InstitutionProfileID,
			DisplayName:              profile.DisplayName,
			AuthenticationProtocolID: profile.AuthenticationProtocolID,
		})
	}
	return summaries, nil
}

func validateProfile(profile InstitutionProfile) error {
	if err := validateInstitutionProfileID(profile.InstitutionProfileID); err != nil {
		return err
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
	return persistence.NewFailure(persistence.FailureInvalidArgument, cause)
}
