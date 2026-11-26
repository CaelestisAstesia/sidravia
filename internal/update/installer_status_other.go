//go:build !windows

package update

import (
	"context"
	"sidravia/internal/update/transaction"
)

func installerStatus(context.Context, transaction.Record) (*installerView, error) { return nil, nil }
