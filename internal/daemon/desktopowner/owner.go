// Package desktopowner observes the exact desktop GUI process that owns a
// desktop-mode daemon. It deliberately has no authority to terminate it.
package desktopowner

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("desktop owner watching is unsupported on this platform")

type Watcher interface {
	Wait(context.Context) error
	Close() error
}
