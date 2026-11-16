//go:build linux

package cli

import (
	"os"
	"syscall"
)

func handoffUpdater(path string, args []string) (bool, error) {
	return true, syscall.Exec(path, append([]string{path}, args...), os.Environ())
}
