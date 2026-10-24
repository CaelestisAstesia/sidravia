//go:build !windows

package lifecycle

import (
	"context"
	"errors"
	"sidravia/internal/clientbootstrap"
)

func Run(context.Context, string, string, uint32, bool, clientbootstrap.Identity) error {
	return errors.New("此平台尚不支持安装生命周期协调")
}
