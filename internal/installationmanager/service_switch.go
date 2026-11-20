package installationmanager

import (
	"context"
	"time"

	"sidravia/internal/productlayout"
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

func BeginServiceSwitch(ctx context.Context) (context.Context, func(), error) {
	return beginServiceSwitch(ctx)
}

func PendingServiceSwitch() (ServiceSwitch, bool, error) {
	return pendingServiceSwitch()
}

func SaveServiceSwitch(ctx context.Context, state ServiceSwitch) error {
	return saveServiceSwitch(ctx, &state)
}

func ClearServiceSwitch(ctx context.Context) error {
	return saveServiceSwitch(ctx, nil)
}

func ReloadServiceSwitch(ctx context.Context) error { return reloadServiceSwitch(ctx) }
