package environment

import "context"

// Observer publishes network Snapshots until the caller's context is canceled.
type Observer interface {
	Observe(context.Context, chan<- Snapshot) error
}

// NewSystemObserver returns the production network Observer for the current
// platform. On Windows it polls real adapter facts at a fixed interval. On
// other platforms Observe returns ErrUnsupported and publishes no Snapshot.
func NewSystemObserver() Observer {
	return newSystemObserver()
}
