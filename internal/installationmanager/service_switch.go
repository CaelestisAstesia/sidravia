package installationmanager

import (
	"time"

	productlayout "sidravia/internal/installation/layout"
)

// ServiceSwitch records the original registration and running intent during a
// handoff. It can contain a saved configuration identifier, never credentials.
type ServiceSwitch struct {
	ID               string               `json:"id"`
	Layout           productlayout.Layout `json:"layout"`
	Mode             string               `json:"mode"`
	OwnerPID         int                  `json:"ownerPid,omitempty"`
	OwnerStart       uint64               `json:"ownerStart,omitempty"`
	ConfigurationID  string               `json:"configurationId,omitempty"`
	Enabled          bool                 `json:"enabled"`
	TargetOwnerPID   int                  `json:"targetOwnerPid,omitempty"`
	TargetOwnerStart uint64               `json:"targetOwnerStart,omitempty"`
	RuntimePID       int                  `json:"runtimePid"`
	Expires          time.Time            `json:"expires"`
	TargetDirectory  string               `json:"targetDirectory,omitempty"`
	SourceVersion    string               `json:"sourceVersion,omitempty"`
	SourceBuild      string               `json:"sourceBuild,omitempty"`
}

func PendingServiceSwitch() (ServiceSwitch, bool, error) {
	return pendingServiceSwitch()
}
