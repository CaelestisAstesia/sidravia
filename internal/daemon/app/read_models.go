package app

import (
	"context"
	"errors"
	"sort"

	"sidravia/internal/daemon/authentication/protocol"
	"sidravia/internal/daemon/authentication/session"
	config "sidravia/internal/daemon/configuration"
	"sidravia/internal/daemon/persistence"
)

// ConfigurationRuntimeAvailability is a read-only description of the selected
// compiled dependencies. It does not grant permission to write or authenticate.
type ConfigurationRuntimeAvailability string

const (
	ConfigurationRuntimeAvailable           ConfigurationRuntimeAvailability = "available"
	ConfigurationRuntimeProfileUnavailable  ConfigurationRuntimeAvailability = "profile_unavailable"
	ConfigurationRuntimeProtocolUnavailable ConfigurationRuntimeAvailability = "protocol_unavailable"
	ConfigurationRuntimeOverrideInvalid     ConfigurationRuntimeAvailability = "override_invalid"
)

func readModelContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("read model context is required")
	}
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), context.Cause(ctx))
	}
	return nil
}

// describeConfiguration keeps a broken persisted row manageable. Write paths
// deliberately continue using strict candidate validation and enrich instead.
func (application *Application) describeConfiguration(ctx context.Context, value config.Configuration) (ConfigurationResult, error) {
	if err := readModelContextError(ctx); err != nil {
		return ConfigurationResult{}, err
	}
	stored, err := application.catalog.HasRecord(ctx, value.ConfigurationID)
	if contextErr := readModelContextError(ctx); contextErr != nil {
		return ConfigurationResult{}, errors.Join(err, contextErr)
	}
	if err != nil {
		return ConfigurationResult{}, err
	}
	result := ConfigurationResult{Configuration: value.Clone(), CredentialStored: stored, StorageProtection: application.catalog.StorageProtection()}
	profile, err := application.profiles.Get(ctx, value.InstitutionProfileID)
	if contextErr := readModelContextError(ctx); contextErr != nil {
		return ConfigurationResult{}, errors.Join(err, contextErr)
	}
	if err != nil {
		var failure *persistence.Failure
		if errors.As(err, &failure) && failure.Code() == persistence.FailureNotFound {
			result.RuntimeAvailability = ConfigurationRuntimeProfileUnavailable
			return result, nil
		}
		return ConfigurationResult{}, err
	}
	result.InstitutionDisplayName = profile.DisplayName
	result.AuthenticationProtocolID = string(profile.AuthenticationProtocolID)
	factory, err := application.authenticationResolver.protocols.GetFactory(profile.AuthenticationProtocolID)
	if contextErr := readModelContextError(ctx); contextErr != nil {
		return ConfigurationResult{}, errors.Join(err, contextErr)
	}
	if err != nil {
		result.RuntimeAvailability = ConfigurationRuntimeProtocolUnavailable
		return result, nil
	}
	result.RuntimeAvailability = ConfigurationRuntimeAvailable
	if err := factory.ValidateProtocolContextOverride(append(protocol.AuthenticationProtocolContextOverride(nil), value.ProtocolContextOverride...)); err != nil {
		result.RuntimeAvailability = ConfigurationRuntimeOverrideInvalid
	}
	if err := readModelContextError(ctx); err != nil {
		return ConfigurationResult{}, err
	}
	return result, nil
}

// SessionListView owns its returned slices and nested Snapshot values. Cleanup
// requirements are projections of the existing Application quarantine map, not
// a second lifecycle state owner or artificial actor revision.
type SessionListView struct {
	Sessions                  []session.Snapshot
	CleanupRequiredSessionIDs []session.AuthenticationSessionID
}

func (application *Application) ListSessionView(ctx context.Context) (SessionListView, error) {
	if err := readModelContextError(ctx); err != nil {
		return SessionListView{}, err
	}
	application.opMu.Lock()
	defer application.opMu.Unlock()
	return application.listSessionViewLocked(ctx)
}

// The caller holds opMu. Future subscription composition can reuse this query
// without recursively acquiring the operation lock.
func (application *Application) listSessionViewLocked(ctx context.Context) (SessionListView, error) {
	if err := readModelContextError(ctx); err != nil {
		return SessionListView{}, err
	}
	snapshots, err := application.sup.List(ctx)
	if err != nil {
		return SessionListView{}, errors.Join(err, context.Cause(ctx))
	}
	if err := readModelContextError(ctx); err != nil {
		return SessionListView{}, err
	}
	result := SessionListView{Sessions: make([]session.Snapshot, 0, len(snapshots)), CleanupRequiredSessionIDs: []session.AuthenticationSessionID{}}
	for _, snapshot := range snapshots {
		result.Sessions = append(result.Sessions, snapshot.Clone())
		if application.invalidSessions[snapshot.AuthenticationSessionID] {
			result.CleanupRequiredSessionIDs = append(result.CleanupRequiredSessionIDs, snapshot.AuthenticationSessionID)
		}
	}
	sort.Slice(result.CleanupRequiredSessionIDs, func(i, j int) bool { return result.CleanupRequiredSessionIDs[i] < result.CleanupRequiredSessionIDs[j] })
	return result, nil
}
