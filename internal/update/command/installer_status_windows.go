//go:build windows

package command

import (
	"context"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/setup"
)

func installerStatus(ctx context.Context, r operations.Record) (*installerView, error) {
	if r.Source.Layout != "installed" {
		return nil, nil
	}
	s, e := setup.ReadSession(ctx, r.Source.ID, r.ID)
	if e != nil {
		return nil, e
	}
	return &installerView{ExitCode: s.InstallerExit, Integration: s.Integration, RestartRequired: s.RestartRequired}, nil
}
