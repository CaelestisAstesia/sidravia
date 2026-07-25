package environment

import "context"

type Observer interface {
	Observe(context.Context, chan<- Snapshot) error
}
