//go:build windows

package update

import (
	"context"
	"sidravia/internal/update/setup/receipt"
	"sidravia/internal/update/transaction"
)

func installerStatus(ctx context.Context, r transaction.Record) (*installerView, error) {
	if r.Source.Layout != "installed" {
		return nil, nil
	}
	s, e := receipt.Status(ctx, r.Source.ID, r.ID)
	if e != nil {
		return nil, e
	}
	return &installerView{ExitCode: s.ExitCode, Integration: s.Integration, RestartRequired: s.RestartRequired}, nil
}
