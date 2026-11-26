//go:build !windows

package inno

import (
	"context"
	"errors"

	"sidravia/internal/background/daemonctl"
)

func Run(context.Context, string, string, uint32, bool, daemonctl.Identity) error {
	return errors.New("此平台尚不支持安装生命周期协调")
}
