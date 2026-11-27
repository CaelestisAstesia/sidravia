//go:build !linux && !darwin && !windows

package transaction

import "errors"

func lock(string) (func(), error) { return nil, errors.New("update operation locking unsupported") }
