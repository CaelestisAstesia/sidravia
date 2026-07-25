//go:build !windows

package host

import (
	"context"
	"errors"
)

// Run starts the daemon host. On non-Windows platforms it returns Unsupported.
func Run(ctx context.Context, cfg Config) error {
	return errors.New("sidraviad: unsupported platform; only Windows is supported")
}
