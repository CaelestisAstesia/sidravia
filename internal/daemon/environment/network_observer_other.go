//go:build !windows

package environment

import "context"

type systemObserver struct{}

func newSystemObserver() Observer {
	return &systemObserver{}
}

// Observe is unsupported on non-Windows platforms. It returns ErrUnsupported
// without publishing a Snapshot, so callers cannot mistake a missing platform
// for a successful empty observation.
func (observer *systemObserver) Observe(ctx context.Context, output chan<- Snapshot) error {
	return ErrUnsupported
}
