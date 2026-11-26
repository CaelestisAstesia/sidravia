//go:build windows

package update

import (
	"context"

	"sidravia/internal/update/setup"
	"sidravia/internal/update/transaction"
)

func installerStatus(ctx context.Context, r transaction.Record) (*installerView, error) {
	if r.Source.Layout != "installed" {
		return nil, nil
	}
	s, e := setup.ReadSession(ctx, r.Source.ID, r.ID)
	if e != nil {
		return nil, e
	}
	return &installerView{ExitCode: s.InstallerExit, Integration: s.Integration, RestartRequired: s.RestartRequired}, nil
}
