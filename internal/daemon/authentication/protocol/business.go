package protocol

import (
	profilevalue "sidravia/internal/connection/profile"
)

// Business support is optional and institution-specific. It does not authorize
// extra network operations or change the authentication lifecycle.
type BusinessCapabilities struct{ Statistics, TextNotifications bool }
type BusinessCapabilityProvider interface {
	BusinessCapabilities(profilevalue.ProtocolConfiguration) (BusinessCapabilities, error)
}

// AccountStatistics is a server login snapshot. TrafficBytes is historical
// billed usage, not a local connection counter or a promised calendar period.
// BalanceCents is denominated in hundredths of yuan. Missing is distinct from 0.
type AccountStatistics struct {
	HasTraffic   bool
	TrafficBytes uint64
	HasBalance   bool
	BalanceCents uint64
}

type BusinessObserver interface {
	ObserveAccountStatistics(AccountStatistics)
	ObserveTextNotification(string)
}
