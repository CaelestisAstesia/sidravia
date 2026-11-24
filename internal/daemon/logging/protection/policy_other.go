//go:build !windows && !linux

package protection

type policy struct{}

func (policy) Directory(string) error { return nil }

func (policy) File(string) error { return nil }

// New prepares the log protection policy for the current process owner.
func New() (Policy, error) {
	return policy{}, nil
}
