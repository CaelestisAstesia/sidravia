//go:build !windows && !linux

package environment

import "context"

type unsupportedSystemObserver struct{}

func newSystemObserver() Observer {
	return &unsupportedSystemObserver{}
}

// Observe is unsupported on non-Linux/non-Windows platforms. It returns
// ErrUnsupported without publishing a Snapshot, so callers cannot mistake a
// missing platform for a successful empty observation.
func (observer *unsupportedSystemObserver) Observe(ctx context.Context, output chan<- Snapshot) error {
	return ErrUnsupported
}
