//go:build !windows

package installationgate

import "errors"

var ErrBusy = errors.New("installation in progress")

func AcquireStart() (func(), error) { return func() {}, nil }
