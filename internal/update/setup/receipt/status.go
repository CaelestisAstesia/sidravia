// Package receipt exposes the read-side evidence of a Setup operation.
package receipt

import (
	"context"
	"sidravia/internal/update/setup/internal/receiptstate"
)

type Outcome struct {
	ExitCode        *uint32
	Integration     string
	RestartRequired bool
}

func Status(ctx context.Context, target, id string) (Outcome, error) {
	s, err := receiptstate.Read(ctx, target, id)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{s.InstallerExit, s.Integration, s.RestartRequired}, nil
}
