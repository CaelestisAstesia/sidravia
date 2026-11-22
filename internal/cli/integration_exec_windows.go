//go:build windows

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type managerOutput struct{ bytes.Buffer }

func (b *managerOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("manager output too large")
	}
	return b.Buffer.Write(p)
}

func invokeInstallationManager(ctx context.Context, args []string) ([]byte, error) {
	path, err := siblingExecutable("sidraviamgr")
	if err != nil {
		return nil, integrationFailure("无法确定维护工具的位置。", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	var output managerOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("维护操作已取消或超时：%w", ctx.Err())
	}
	if err != nil && output.Len() == 0 {
		return nil, integrationFailure("维护工具无法运行，请检查安装是否完整。", err)
	}
	return output.Bytes(), err
}
