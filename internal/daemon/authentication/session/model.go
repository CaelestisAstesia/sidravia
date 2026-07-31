package session

import (
	"net/netip"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
	profile "sidravia/internal/daemon/configuration"
	environment "sidravia/internal/daemon/environment"
)

type AuthenticationSessionID string

type NetworkBindingPolicyMode string

const AutomaticallySelectLatestAvailable NetworkBindingPolicyMode = "automatically_select_latest_available"

type NetworkBindingPolicy struct {
	Mode NetworkBindingPolicyMode
}

type Configuration struct {
	AuthenticationSessionID AuthenticationSessionID
	DisplayName             string
	InstitutionProfileID    profile.InstitutionProfileID
	NetworkBindingPolicy    NetworkBindingPolicy
	ProtocolContextOverride protocol.AuthenticationProtocolContextOverride
}

type Intent string

const (
	MaintainAuthentication Intent = "maintain_authentication"
	SuspendAuthentication  Intent = "suspend_authentication"
)

type State string

const (
	Suspended          State = "suspended"
	WaitingForNetwork  State = "waiting_for_network"
	Authenticating     State = "authenticating"
	Authenticated      State = "authenticated"
	WaitingBeforeRetry State = "waiting_before_retry"
	BlockedByError     State = "blocked_by_error"
	Stopping           State = "stopping"
)

const (
	StateReasonCodeNetworkUnavailable           = "network_unavailable"
	StateReasonCodeRuntimeDefinitionUnavailable = "runtime_definition_unavailable"
	StateReasonCodeProtocolRunCreationFailed    = "protocol_run_creation_failed"
	StateReasonCodeProtocolRunFailed            = "protocol_run_failed"
	StateReasonCodeProtocolContractViolated     = "protocol_contract_violated"
	StateReasonCodeAutomaticReconnectDisabled   = "automatic_reconnect_disabled"
)

type StateReason struct {
	Code        string
	Description string
}

type AuthenticationFailure struct {
	Code                   protocol.AuthenticationProtocolFailureCode
	Description            string
	HandlingRecommendation protocol.AuthenticationProtocolFailureHandlingRecommendation
}

type NetworkBindingSummary struct {
	InterfaceID      environment.InterfaceID
	DisplayName      string
	LocalIPv4Address netip.Addr
}

type Snapshot struct {
	AuthenticationSessionID     AuthenticationSessionID
	DisplayName                 string
	InstitutionProfileID        profile.InstitutionProfileID
	InstitutionDisplayName      string
	AuthenticationProtocolID    protocol.AuthenticationProtocolID
	AccountName                 string
	Intent                      Intent
	State                       State
	StateReason                 *StateReason
	SelectedNetworkBinding      *NetworkBindingSummary
	AuthenticationEstablishedAt *time.Time
	NextRetryAt                 *time.Time
	LastAuthenticationFailure   *AuthenticationFailure
	Revision                    uint64
	UpdatedAt                   time.Time
}

func (snapshot Snapshot) Clone() Snapshot {
	cloned := snapshot
	cloned.StateReason = cloneStateReason(snapshot.StateReason)
	cloned.SelectedNetworkBinding = cloneNetworkBindingSummary(snapshot.SelectedNetworkBinding)
	cloned.AuthenticationEstablishedAt = cloneTime(snapshot.AuthenticationEstablishedAt)
	cloned.NextRetryAt = cloneTime(snapshot.NextRetryAt)
	cloned.LastAuthenticationFailure = cloneAuthenticationFailure(snapshot.LastAuthenticationFailure)
	return cloned
}

func cloneStateReason(reason *StateReason) *StateReason {
	if reason == nil {
		return nil
	}
	cloned := *reason
	return &cloned
}

func cloneNetworkBindingSummary(summary *NetworkBindingSummary) *NetworkBindingSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAuthenticationFailure(failure *AuthenticationFailure) *AuthenticationFailure {
	if failure == nil {
		return nil
	}
	cloned := *failure
	return &cloned
}
