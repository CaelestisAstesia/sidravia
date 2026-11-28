//go:build !linux && !windows

package startgate

// AcquireRun preserves installed-platform short startup admission. Those
// installers coordinate normal process exit separately from this gate.
func AcquireRun() (func(), error) {
	release, err := AcquireStart()
	if err != nil {
		return nil, err
	}
	release()
	return func() {}, nil
}
