//go:build !windows

package cli

import (
	"context"
	"errors"
)

func invokeInstallationManager(context.Context, []string) ([]byte, error) {
	return nil, errors.New("当前平台尚不支持安装集成管理。")
}
