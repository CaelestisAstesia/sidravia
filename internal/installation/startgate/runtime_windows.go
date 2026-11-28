//go:build windows

package startgate

func AcquireRun() (func(), error) {
	release, err := AcquireStart()
	if err != nil {
		return nil, err
	}
	release()
	return func() {}, nil
}
