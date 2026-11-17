//go:build !windows

package command

import (
	"context"
	"sidravia/internal/update/operations"
)

func installerStatus(context.Context, operations.Record) (*installerView, error) { return nil, nil }
