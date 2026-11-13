//go:build linux

package cli

import (
	"context"
	"os/exec"
	"time"
)

func openLogDirectoryNative(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "xdg-open", path).Run()
}
